package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	engineInfoRunCommand = runEngineInfoCommand
	engineRuntimeGOOS    = func() string { return runtime.GOOS }
	engineRuntimeGOARCH  = func() string { return runtime.GOARCH }
)

type engineInfo struct {
	LlamaServerVersion  string   `json:"llama_server_version"`
	RuntimeGOOS         string   `json:"runtime_goos"`
	RuntimeGOARCH       string   `json:"runtime_goarch"`
	Platform            string   `json:"platform"`
	Architecture        string   `json:"architecture"`
	ConfiguredEngine    string   `json:"configured_engine"`
	EffectiveEngine     string   `json:"effective_engine"`
	EffectiveLLMLibrary string   `json:"effective_llm_library"`
	EngineSource        string   `json:"engine_source"`
	EngineConfigPath    string   `json:"engine_config_path"`
	MetalGPUDevice      string   `json:"metal_gpu_device"`
	SupportedQuantTypes []string `json:"supported_quant_types"`
	GGMLBackends        []string `json:"ggml_backends"`
	LlamaServerPath     string   `json:"llama_server_path"`
	LlamaServerSize     int64    `json:"llama_server_size_bytes"`
	DylibCount          int      `json:"dylib_count"`
	MLXAvailable        bool     `json:"mlx_available"`
	MLXVariants         []string `json:"mlx_variants"`
	MLXEligibleVariants []string `json:"mlx_eligible_variants"`
	MLXFeasibility      string   `json:"mlx_feasibility"`
}

type engineCurrentInfo struct {
	ConfiguredEngine     string `json:"configured_engine"`
	ConfiguredLLMLibrary string `json:"configured_llm_library"`
	EffectiveEngine      string `json:"effective_engine"`
	EffectiveLLMLibrary  string `json:"effective_llm_library"`
	Source               string `json:"source"`
	ConfigPath           string `json:"config_path"`
	ConfigExists         bool   `json:"config_exists"`
	RuntimeGOOS          string `json:"runtime_goos"`
	RuntimeGOARCH        string `json:"runtime_goarch"`
	Platform             string `json:"platform"`
	Architecture         string `json:"architecture"`
}

type engineSelection struct {
	Name        string
	LLMLibrary  string
	Description string
	Warning     string
}

type enginePersistedConfig struct {
	Engine     string
	LLMLibrary string
	Path       string
	Exists     bool
}

type engineMLXReport struct {
	Available        bool     `json:"available"`
	LibDir           string   `json:"lib_dir"`
	RuntimeGOOS      string   `json:"runtime_goos"`
	RuntimeGOARCH    string   `json:"runtime_goarch"`
	Platform         string   `json:"platform"`
	Architecture     string   `json:"architecture"`
	Variants         []string `json:"variants"`
	EligibleVariants []string `json:"eligible_variants"`
	Verdict          string   `json:"verdict"`
	UsableSubset     []string `json:"usable_subset"`
	BlockingGaps     []string `json:"blocking_gaps"`
}

type enginePlatformInfo struct {
	RuntimeGOOS   string
	RuntimeGOARCH string
	Platform      string
	Architecture  string
}

func engineCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "engine",
		Short: "Inspect the local Ocelot engine",
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show local puma.cpp engine information",
		Args:  cobra.ExactArgs(0),
		RunE:  engineInfoHandler,
	}
	infoCmd.Flags().Bool("json", false, "Output engine information as JSON")

	currentCmd := &cobra.Command{
		Use:   "current",
		Short: "Show the configured Ocelot engine selection",
		Args:  cobra.ExactArgs(0),
		RunE:  engineCurrentHandler,
	}
	currentCmd.Flags().Bool("json", false, "Output current engine selection as JSON")

	useCmd := &cobra.Command{
		Use:   "use ENGINE",
		Short: "Persist the engine selection used by the Ocelot launcher",
		Long:  "Persist the engine selection used by the Ocelot launcher. Valid engines: puma, metal, intel-metal, intel-cpu, cpu, auto, mlx, or an installed mlx_* variant.",
		Args:  cobra.ExactArgs(1),
		RunE:  engineUseHandler,
	}

	mlxCmd := &cobra.Command{
		Use:   "mlx",
		Short: "Show whether MLX can be used by this Ocelot install",
		Args:  cobra.ExactArgs(0),
		RunE:  engineMLXHandler,
	}
	mlxCmd.Flags().Bool("json", false, "Output MLX feasibility as JSON")

	cmd.AddCommand(infoCmd, currentCmd, useCmd, mlxCmd)
	return cmd
}

func engineInfoHandler(cmd *cobra.Command, _ []string) error {
	info, err := collectEngineInfo(cmd.Context())
	if err != nil {
		return err
	}

	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)
	}

	printEngineInfo(cmd, info)
	return nil
}

func collectEngineInfo(ctx context.Context) (engineInfo, error) {
	platform := currentEnginePlatform()

	libDir, err := ocelotEngineLibDir()
	if err != nil {
		return engineInfo{}, err
	}

	serverPath := filepath.Join(libDir, "llama-server")
	stat, err := os.Stat(serverPath)
	if err != nil {
		return engineInfo{}, fmt.Errorf("stat llama-server: %w", err)
	}

	versionOutput, err := engineInfoRunCommand(ctx, serverPath, "--version")
	if err != nil {
		return engineInfo{}, fmt.Errorf("run llama-server --version: %w", err)
	}

	gpuOutput, err := engineInfoRunCommand(ctx, "sh", "-c", "system_profiler SPDisplaysDataType | grep Chipset | head -1")
	if err != nil {
		return engineInfo{}, fmt.Errorf("detect Metal GPU device: %w", err)
	}

	dylibs, err := filepath.Glob(filepath.Join(libDir, "*.dylib"))
	if err != nil {
		return engineInfo{}, fmt.Errorf("count dylibs: %w", err)
	}

	current, err := resolveCurrentEngine()
	if err != nil {
		return engineInfo{}, err
	}

	mlxReport, err := collectEngineMLXReport()
	if err != nil {
		return engineInfo{}, err
	}

	return engineInfo{
		LlamaServerVersion:  parseLlamaServerVersion(versionOutput),
		RuntimeGOOS:         platform.RuntimeGOOS,
		RuntimeGOARCH:       platform.RuntimeGOARCH,
		Platform:            platform.Platform,
		Architecture:        platform.Architecture,
		ConfiguredEngine:    current.ConfiguredEngine,
		EffectiveEngine:     current.EffectiveEngine,
		EffectiveLLMLibrary: current.EffectiveLLMLibrary,
		EngineSource:        current.Source,
		EngineConfigPath:    current.ConfigPath,
		MetalGPUDevice:      parseChipsetModel(gpuOutput),
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     serverPath,
		LlamaServerSize:     stat.Size(),
		DylibCount:          len(dylibs),
		MLXAvailable:        mlxReport.Available,
		MLXVariants:         mlxReport.Variants,
		MLXEligibleVariants: mlxReport.EligibleVariants,
		MLXFeasibility:      mlxReport.Verdict,
	}, nil
}

func ocelotEngineLibDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("OCELOT_HOME")); home != "" {
		return filepath.Join(home, "lib", "ollama"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "ocelot", "lib", "ollama"), nil
}

func engineCurrentHandler(cmd *cobra.Command, _ []string) error {
	current, err := resolveCurrentEngine()
	if err != nil {
		return err
	}

	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(current)
	}

	printEngineCurrent(cmd, current)
	return nil
}

func engineUseHandler(cmd *cobra.Command, args []string) error {
	selection, err := resolveEngineSelection(args[0])
	if err != nil {
		return err
	}

	path, err := writeEngineConfig(selection)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Selected engine: %s\n", selection.Name)
	fmt.Fprintf(out, "OLLAMA_LLM_LIBRARY: %s\n", displayLLMLibrary(selection.LLMLibrary))
	fmt.Fprintf(out, "Config: %s\n", path)
	fmt.Fprintln(out, "Restart the running service for server changes:")
	fmt.Fprintln(out, "  launchctl unload ~/Library/LaunchAgents/com.ocelot.server.plist")
	fmt.Fprintln(out, "  launchctl load ~/Library/LaunchAgents/com.ocelot.server.plist")
	if selection.Warning != "" {
		fmt.Fprintf(out, "Note: %s\n", selection.Warning)
	}
	return nil
}

func engineMLXHandler(cmd *cobra.Command, _ []string) error {
	report, err := collectEngineMLXReport()
	if err != nil {
		return err
	}

	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	printEngineMLXReport(cmd, report)
	return nil
}

func runEngineInfoCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w\n%s", strings.Join(append([]string{name}, args...), " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseLlamaServerVersion(output string) string {
	fallback := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		}
		if fallback == "" {
			fallback = line
		}
	}
	return fallback
}

func parseChipsetModel(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if before, after, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(before), "Chipset Model") {
			return strings.TrimSpace(after)
		}
		return line
	}
	return ""
}

func currentEnginePlatform() enginePlatformInfo {
	goos := strings.TrimSpace(engineRuntimeGOOS())
	goarch := strings.TrimSpace(engineRuntimeGOARCH())
	if goos == "" {
		goos = "unknown"
	}
	if goarch == "" {
		goarch = "unknown"
	}
	return enginePlatformInfo{
		RuntimeGOOS:   goos,
		RuntimeGOARCH: goarch,
		Platform:      goos + "/" + goarch,
		Architecture:  engineArchitectureLabel(goos, goarch),
	}
}

func engineArchitectureLabel(goos, goarch string) string {
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "apple-silicon"
	case goos == "darwin" && goarch == "amd64":
		return "intel"
	default:
		return goarch
	}
}

func enginePlatformDescription(platform enginePlatformInfo) string {
	return platform.Platform + " (" + platform.Architecture + ")"
}

func supportedEngineQuantTypes() []string {
	return []string{"tq1_0", "tq2_0", "tq3_1s", "q4_0", "q4_k_m", "q8_0", "f16"}
}

func engineConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("OCELOT_ENGINE_CONFIG")); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "ocelot", "engine.env"), nil
}

func readEngineConfig() (enginePersistedConfig, error) {
	path, err := engineConfigPath()
	if err != nil {
		return enginePersistedConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return enginePersistedConfig{Path: path}, nil
		}
		return enginePersistedConfig{}, fmt.Errorf("read engine config: %w", err)
	}

	cfg := parseEngineConfig(string(data))
	cfg.Path = path
	cfg.Exists = true
	return cfg, nil
}

func parseEngineConfig(contents string) enginePersistedConfig {
	var cfg enginePersistedConfig
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = parseEngineConfigValue(value)
		switch strings.TrimSpace(key) {
		case "OCELOT_ENGINE":
			cfg.Engine = strings.ToLower(strings.TrimSpace(value))
		case "OLLAMA_LLM_LIBRARY":
			cfg.LLMLibrary = strings.TrimSpace(value)
		}
	}
	return cfg
}

func parseEngineConfigValue(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, `"'`)
}

func writeEngineConfig(selection engineSelection) (string, error) {
	path, err := engineConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create engine config directory: %w", err)
	}

	var b strings.Builder
	b.WriteString("# Generated by `ocelot engine use`; sourced by ~/.local/bin/ocelot.\n")
	fmt.Fprintf(&b, "export OCELOT_ENGINE=%s\n", strconv.Quote(selection.Name))
	if selection.Name == "auto" {
		b.WriteString("unset OLLAMA_LLM_LIBRARY\n")
	} else {
		fmt.Fprintf(&b, "export OLLAMA_LLM_LIBRARY=%s\n", strconv.Quote(selection.LLMLibrary))
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write engine config: %w", err)
	}
	return path, nil
}

func resolveCurrentEngine() (engineCurrentInfo, error) {
	cfg, err := readEngineConfig()
	if err != nil {
		return engineCurrentInfo{}, err
	}

	platform := currentEnginePlatform()
	current := engineCurrentInfo{
		ConfiguredEngine:     cfg.Engine,
		ConfiguredLLMLibrary: cfg.LLMLibrary,
		ConfigPath:           cfg.Path,
		ConfigExists:         cfg.Exists,
		RuntimeGOOS:          platform.RuntimeGOOS,
		RuntimeGOARCH:        platform.RuntimeGOARCH,
		Platform:             platform.Platform,
		Architecture:         platform.Architecture,
	}

	if llmLibrary, ok := os.LookupEnv("OLLAMA_LLM_LIBRARY"); ok {
		current.EffectiveLLMLibrary = strings.TrimSpace(llmLibrary)
		if err := validateLLMLibraryForPlatform(current.EffectiveLLMLibrary); err != nil {
			return engineCurrentInfo{}, err
		}
		current.EffectiveEngine = engineNameForLLMLibrary(current.EffectiveLLMLibrary)
		current.Source = "environment"
		if current.ConfiguredEngine == "" {
			current.ConfiguredEngine = current.EffectiveEngine
		}
		return current, nil
	}

	if cfg.Exists {
		if cfg.Engine == "" && cfg.LLMLibrary == "" {
			defaultEngine := defaultEngineName()
			current.ConfiguredEngine = defaultEngine
			current.ConfiguredLLMLibrary = "metal"
			current.EffectiveEngine = defaultEngine
			current.EffectiveLLMLibrary = "metal"
			current.Source = "default"
			return current, nil
		}

		current.Source = "config"
		current.EffectiveLLMLibrary = cfg.LLMLibrary
		if cfg.Engine != "" {
			current.EffectiveEngine = cfg.Engine
		} else {
			current.EffectiveEngine = engineNameForLLMLibrary(cfg.LLMLibrary)
		}
		if current.ConfiguredEngine == "" {
			current.ConfiguredEngine = current.EffectiveEngine
		}
		return current, nil
	}

	defaultEngine := defaultEngineName()
	current.ConfiguredEngine = defaultEngine
	current.ConfiguredLLMLibrary = "metal"
	current.EffectiveEngine = defaultEngine
	current.EffectiveLLMLibrary = "metal"
	current.Source = "default"
	return current, nil
}

func defaultEngineName() string {
	return engineNameForLLMLibrary("metal")
}

func engineNameForLLMLibrary(llmLibrary string) string {
	llmLibrary = strings.TrimSpace(strings.ToLower(llmLibrary))
	switch {
	case llmLibrary == "":
		return "auto"
	case llmLibrary == "metal":
		platform := currentEnginePlatform()
		if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
			return "intel-metal"
		}
		return "puma"
	case llmLibrary == "cpu" || strings.HasPrefix(llmLibrary, "cpu_"):
		platform := currentEnginePlatform()
		if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
			return "intel-cpu"
		}
		return "cpu"
	case strings.HasPrefix(llmLibrary, "mlx_") && isEligibleMLXVariant(llmLibrary):
		return "mlx"
	case strings.HasPrefix(llmLibrary, "mlx_"):
		return "custom"
	default:
		return "custom"
	}
}

func validateLLMLibraryForPlatform(llmLibrary string) error {
	name := strings.TrimSpace(strings.ToLower(llmLibrary))
	if !strings.HasPrefix(name, "mlx_") || isEligibleMLXVariant(name) {
		return nil
	}

	platform := currentEnginePlatform()
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
		return fmt.Errorf("OLLAMA_LLM_LIBRARY %q is not eligible on %s; Intel Macs support mlx_metal_v3 only and never auto-select mlx_metal_v4. GGUF models continue to route to puma.cpp / llama-server", llmLibrary, enginePlatformDescription(platform))
	}
	return fmt.Errorf("OLLAMA_LLM_LIBRARY %q is not eligible on %s; GGUF models continue to route to puma.cpp / llama-server", llmLibrary, enginePlatformDescription(platform))
}

func resolveEngineSelection(name string) (engineSelection, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "puma", "metal":
		return engineSelection{
			Name:        "puma",
			LLMLibrary:  "metal",
			Description: "puma.cpp / llama-server using the Metal GGML backend",
		}, nil
	case "intel-metal":
		return engineSelection{
			Name:        "intel-metal",
			LLMLibrary:  "metal",
			Description: "puma.cpp / llama-server using the Metal GGML backend on Intel Mac",
			Warning:     intelTargetWarning("intel-metal"),
		}, nil
	case "intel-cpu":
		return engineSelection{
			Name:        "intel-cpu",
			LLMLibrary:  "cpu",
			Description: "puma.cpp / llama-server using the CPU GGML backend on Intel Mac",
			Warning:     intelTargetWarning("intel-cpu"),
		}, nil
	case "cpu":
		return engineSelection{
			Name:        "cpu",
			LLMLibrary:  "cpu",
			Description: "puma.cpp / llama-server using the CPU GGML backend; portable alias for intel-cpu on Intel Mac",
		}, nil
	case "auto":
		return engineSelection{
			Name:        "auto",
			Description: "leave OLLAMA_LLM_LIBRARY unset so ocelot can autodetect the GGML backend",
		}, nil
	case "mlx":
		libDir, err := ocelotEngineLibDir()
		if err != nil {
			return engineSelection{}, err
		}
		variants, err := detectMLXVariants(libDir)
		if err != nil {
			return engineSelection{}, err
		}
		eligibleVariants := eligibleMLXVariants(variants)
		if len(eligibleVariants) == 0 {
			return engineSelection{}, noEligibleMLXRuntimeError(libDir, variants)
		}
		return engineSelection{
			Name:        "mlx",
			LLMLibrary:  eligibleVariants[0],
			Description: "experimental MLX runtime for safetensors LLM models",
			Warning:     "MLX is safetensors-only and not a full GGUF/Ollama engine; puma.cpp remains required for GGUF, embeddings, detokenize, and native llama-server chat templates.",
		}, nil
	default:
		if strings.HasPrefix(name, "mlx_") {
			libDir, err := ocelotEngineLibDir()
			if err != nil {
				return engineSelection{}, err
			}
			if !engineVariantInstalled(libDir, name) {
				return engineSelection{}, fmt.Errorf("MLX variant %q is not installed under %s", name, libDir)
			}
			if !isEligibleMLXVariant(name) {
				platform := currentEnginePlatform()
				return engineSelection{}, fmt.Errorf("MLX variant %q is installed but is not eligible on %s; Intel Macs support mlx_metal_v3 only and never auto-select mlx_metal_v4. GGUF models continue to route to puma.cpp / llama-server", name, enginePlatformDescription(platform))
			}
			return engineSelection{
				Name:        "mlx",
				LLMLibrary:  name,
				Description: "experimental MLX runtime variant for safetensors LLM models",
				Warning:     "MLX is safetensors-only and not a full GGUF/Ollama engine; puma.cpp remains required for GGUF, embeddings, detokenize, and native llama-server chat templates.",
			}, nil
		}
		return engineSelection{}, fmt.Errorf("unsupported engine %q; choose puma, metal, intel-metal, intel-cpu, cpu, auto, mlx, or an installed mlx_* variant", name)
	}
}

func intelTargetWarning(engine string) string {
	platform := currentEnginePlatform()
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
		return ""
	}
	return fmt.Sprintf("%s is an explicit Intel Mac target; persisted as cross-target config from current %s", engine, enginePlatformDescription(platform))
}

func detectMLXVariants(libDir string) ([]string, error) {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read MLX runtime directory: %w", err)
	}
	var variants []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "mlx_") {
			variants = append(variants, entry.Name())
		}
	}
	sort.Slice(variants, func(i, j int) bool {
		return mlxVariantComesBefore(variants[i], variants[j])
	})
	return variants, nil
}

func mlxVariantComesBefore(left, right string) bool {
	leftPrefix, leftVersion, leftOK := mlxVariantNumericSuffix(left)
	rightPrefix, rightVersion, rightOK := mlxVariantNumericSuffix(right)
	if leftOK && rightOK && leftPrefix == rightPrefix && leftVersion != rightVersion {
		return leftVersion > rightVersion
	}
	if leftOK != rightOK {
		return leftOK
	}
	return left > right
}

func mlxVariantNumericSuffix(name string) (string, int, bool) {
	idx := strings.LastIndex(strings.ToLower(name), "_v")
	if idx == -1 || idx+2 >= len(name) {
		return "", 0, false
	}
	version, err := strconv.Atoi(name[idx+2:])
	if err != nil {
		return "", 0, false
	}
	return strings.ToLower(name[:idx+2]), version, true
}

func engineVariantInstalled(libDir, variant string) bool {
	info, err := os.Stat(filepath.Join(libDir, variant))
	return err == nil && info.IsDir()
}

func collectEngineMLXReport() (engineMLXReport, error) {
	platform := currentEnginePlatform()
	libDir, err := ocelotEngineLibDir()
	if err != nil {
		return engineMLXReport{}, err
	}
	variants, err := detectMLXVariants(libDir)
	if err != nil {
		return engineMLXReport{}, err
	}
	eligibleVariants := eligibleMLXVariants(variants)
	return engineMLXReport{
		Available:        len(eligibleVariants) > 0,
		LibDir:           libDir,
		RuntimeGOOS:      platform.RuntimeGOOS,
		RuntimeGOARCH:    platform.RuntimeGOARCH,
		Platform:         platform.Platform,
		Architecture:     platform.Architecture,
		Variants:         variants,
		EligibleVariants: eligibleVariants,
		Verdict:          mlxFeasibilityVerdict(variants, eligibleVariants, platform),
		UsableSubset: []string{
			"safetensors LLM models that have an x/mlxrunner model implementation",
			"completion-style generation through the MLX runner client",
			"rendered chat flows that can be reduced to completion prompts",
		},
		BlockingGaps: mlxBlockingGaps(eligibleVariants, platform),
	}, nil
}

func eligibleMLXVariants(variants []string) []string {
	var eligible []string
	for _, variant := range variants {
		if isEligibleMLXVariant(variant) {
			eligible = append(eligible, variant)
		}
	}
	return eligible
}

func isEligibleMLXVariant(variant string) bool {
	platform := currentEnginePlatform()
	name := strings.ToLower(strings.TrimSpace(variant))
	if name == "" {
		return false
	}
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
		return name == "mlx_metal_v3"
	}
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "arm64" {
		return strings.HasPrefix(name, "mlx_metal_v")
	}
	if platform.RuntimeGOOS == "darwin" {
		return strings.HasPrefix(name, "mlx_metal_v")
	}
	return true
}

func noEligibleMLXRuntimeError(libDir string, variants []string) error {
	platform := currentEnginePlatform()
	if len(variants) == 0 {
		return fmt.Errorf("MLX runtime libraries are not installed under %s; MLX remains non-default and is not selectable on %s. GGUF models continue to route to puma.cpp / llama-server", libDir, enginePlatformDescription(platform))
	}
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
		return fmt.Errorf("no eligible MLX runtime is installed under %s for %s; installed variants are %s. Intel Macs support mlx_metal_v3 only, so mlx_metal_v4 is ignored. GGUF models continue to route to puma.cpp / llama-server", libDir, enginePlatformDescription(platform), strings.Join(variants, ", "))
	}
	return fmt.Errorf("no eligible MLX runtime is installed under %s for %s; installed variants are %s. GGUF models continue to route to puma.cpp / llama-server", libDir, enginePlatformDescription(platform), strings.Join(variants, ", "))
}

func mlxFeasibilityVerdict(variants, eligibleVariants []string, platform enginePlatformInfo) string {
	if len(eligibleVariants) == 0 {
		if len(variants) == 0 {
			return fmt.Sprintf("MLX remains non-default and no mlx_* runtime libraries are installed for %s; GGUF models still route to puma.cpp / llama-server.", enginePlatformDescription(platform))
		}
		if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
			return fmt.Sprintf("MLX remains non-default; Intel Mac %s can use mlx_metal_v3 only, but installed variants are %s. GGUF models still route to puma.cpp / llama-server.", enginePlatformDescription(platform), strings.Join(variants, ", "))
		}
		return fmt.Sprintf("MLX remains non-default; no installed MLX runtime is eligible for %s (installed: %s). GGUF models still route to puma.cpp / llama-server.", enginePlatformDescription(platform), strings.Join(variants, ", "))
	}
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
		return "MLX remains non-default; Intel Mac darwin/amd64 is eligible only for mlx_metal_v3. GGUF models still route to puma.cpp / llama-server."
	}
	if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "arm64" {
		return fmt.Sprintf("MLX remains non-default; Apple Silicon %s will use newest eligible installed variant %s for supported safetensors flows. GGUF models still route to puma.cpp / llama-server.", enginePlatformDescription(platform), eligibleVariants[0])
	}
	return fmt.Sprintf("MLX remains non-default; newest eligible installed variant is %s for supported safetensors flows. GGUF models still route to puma.cpp / llama-server.", eligibleVariants[0])
}

func mlxBlockingGaps(eligibleVariants []string, platform enginePlatformInfo) []string {
	gaps := []string{
		"GGUF models still route to puma.cpp / llama-server",
		"native MLX Chat and ApplyChatTemplate are unsupported",
		"MLX Detokenize and Embedding are unsupported",
	}
	if len(eligibleVariants) == 0 {
		if platform.RuntimeGOOS == "darwin" && platform.RuntimeGOARCH == "amd64" {
			return append([]string{"install mlx_metal_v3; mlx_metal_v4 is not eligible on Intel Mac"}, gaps...)
		}
		return append([]string{"install an eligible mlx_* runtime before MLX can be selected"}, gaps...)
	}
	return gaps
}

func displayLLMLibrary(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unset; autodetect)"
	}
	return value
}

func printEngineInfo(cmd *cobra.Command, info engineInfo) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "puma.cpp / llama-server version: %s\n", info.LlamaServerVersion)
	fmt.Fprintf(out, "Runtime: %s/%s\n", info.RuntimeGOOS, info.RuntimeGOARCH)
	fmt.Fprintf(out, "Architecture: %s\n", info.Architecture)
	fmt.Fprintf(out, "Configured engine: %s\n", info.ConfiguredEngine)
	fmt.Fprintf(out, "Effective engine: %s\n", info.EffectiveEngine)
	fmt.Fprintf(out, "OLLAMA_LLM_LIBRARY: %s\n", displayLLMLibrary(info.EffectiveLLMLibrary))
	fmt.Fprintf(out, "Engine source: %s\n", info.EngineSource)
	fmt.Fprintf(out, "Engine config: %s\n", info.EngineConfigPath)
	fmt.Fprintf(out, "Metal GPU device: %s\n", info.MetalGPUDevice)
	fmt.Fprintf(out, "Supported quant types: %s\n", strings.Join(info.SupportedQuantTypes, ", "))
	fmt.Fprintf(out, "GGML backends: %s\n", strings.Join(info.GGMLBackends, ", "))
	fmt.Fprintf(out, "llama-server path: %s\n", info.LlamaServerPath)
	fmt.Fprintf(out, "llama-server size: %s bytes\n", strconv.FormatInt(info.LlamaServerSize, 10))
	fmt.Fprintf(out, "dylib count: %d\n", info.DylibCount)
	if len(info.MLXVariants) == 0 {
		fmt.Fprintf(out, "MLX variants: none installed\n")
	} else {
		fmt.Fprintf(out, "MLX variants: %s\n", strings.Join(info.MLXVariants, ", "))
	}
	if len(info.MLXEligibleVariants) == 0 {
		fmt.Fprintf(out, "MLX eligible variants: none for %s\n", info.Platform)
	} else {
		fmt.Fprintf(out, "MLX eligible variants: %s\n", strings.Join(info.MLXEligibleVariants, ", "))
	}
	fmt.Fprintf(out, "MLX feasibility: %s\n", info.MLXFeasibility)
}

func printEngineCurrent(cmd *cobra.Command, current engineCurrentInfo) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Runtime: %s/%s\n", current.RuntimeGOOS, current.RuntimeGOARCH)
	fmt.Fprintf(out, "Architecture: %s\n", current.Architecture)
	fmt.Fprintf(out, "Configured engine: %s\n", current.ConfiguredEngine)
	fmt.Fprintf(out, "Configured OLLAMA_LLM_LIBRARY: %s\n", displayLLMLibrary(current.ConfiguredLLMLibrary))
	fmt.Fprintf(out, "Effective engine: %s\n", current.EffectiveEngine)
	fmt.Fprintf(out, "Effective OLLAMA_LLM_LIBRARY: %s\n", displayLLMLibrary(current.EffectiveLLMLibrary))
	fmt.Fprintf(out, "Source: %s\n", current.Source)
	fmt.Fprintf(out, "Config path: %s\n", current.ConfigPath)
}

func printEngineMLXReport(cmd *cobra.Command, report engineMLXReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Runtime: %s/%s\n", report.RuntimeGOOS, report.RuntimeGOARCH)
	fmt.Fprintf(out, "Architecture: %s\n", report.Architecture)
	if len(report.Variants) == 0 {
		fmt.Fprintf(out, "MLX runtime libraries: none installed under %s\n", report.LibDir)
	} else {
		fmt.Fprintf(out, "MLX runtime libraries: %s\n", strings.Join(report.Variants, ", "))
	}
	if len(report.EligibleVariants) == 0 {
		fmt.Fprintf(out, "Eligible MLX variants: none for %s\n", report.Platform)
	} else {
		fmt.Fprintf(out, "Eligible MLX variants: %s\n", strings.Join(report.EligibleVariants, ", "))
	}
	fmt.Fprintf(out, "Verdict: %s\n", report.Verdict)
	fmt.Fprintf(out, "Usable subset:\n")
	for _, item := range report.UsableSubset {
		fmt.Fprintf(out, "  - %s\n", item)
	}
	fmt.Fprintf(out, "Blocking gaps:\n")
	for _, item := range report.BlockingGaps {
		fmt.Fprintf(out, "  - %s\n", item)
	}
}
