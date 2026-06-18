package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseLlamaServerVersion(t *testing.T) {
	got := parseLlamaServerVersion("version: 9568 (e0c1f9aad)\nbuilt with AppleClang 21.0.0.21000101 for Darwin arm64\n")
	want := "9568 (e0c1f9aad)"
	if got != want {
		t.Fatalf("parseLlamaServerVersion() = %q, want %q", got, want)
	}
}

func TestParseLlamaServerVersionPrefersVersionLine(t *testing.T) {
	got := parseLlamaServerVersion("ggml_metal_device_init: tensor API disabled for pre-M5 and pre-A19 devices\nversion: 9568 (e0c1f9aad)\n")
	want := "9568 (e0c1f9aad)"
	if got != want {
		t.Fatalf("parseLlamaServerVersion() = %q, want %q", got, want)
	}
}

func TestParseChipsetModel(t *testing.T) {
	got := parseChipsetModel("      Chipset Model: Apple M3 Ultra\n")
	want := "Apple M3 Ultra"
	if got != want {
		t.Fatalf("parseChipsetModel() = %q, want %q", got, want)
	}
}

func TestPrintEngineInfo(t *testing.T) {
	info := engineInfo{
		LlamaServerVersion:  "9568 (e0c1f9aad)",
		RuntimeGOOS:         "darwin",
		RuntimeGOARCH:       "arm64",
		Platform:            "darwin/arm64",
		Architecture:        "apple-silicon",
		ConfiguredEngine:    "puma",
		EffectiveEngine:     "puma",
		EffectiveLLMLibrary: "metal",
		EngineSource:        "default",
		EngineConfigPath:    "/Users/test/.config/ocelot/engine.env",
		MetalGPUDevice:      "Apple M3 Ultra",
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     "/Users/test/.local/share/ocelot/lib/ollama/llama-server",
		LlamaServerSize:     9167312,
		DylibCount:          50,
		MLXAvailable:        true,
		MLXVariants:         []string{"mlx_metal_v3"},
		MLXEligibleVariants: []string{"mlx_metal_v3"},
		MLXFeasibility:      "MLX remains non-default; Apple Silicon darwin/arm64 (apple-silicon) will use newest eligible installed variant mlx_metal_v3 for supported safetensors flows. GGUF models still route to puma.cpp / llama-server.",
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	printEngineInfo(cmd, info)

	want := "puma.cpp / llama-server version: 9568 (e0c1f9aad)\n" +
		"Runtime: darwin/arm64\n" +
		"Architecture: apple-silicon\n" +
		"Configured engine: puma\n" +
		"Effective engine: puma\n" +
		"OLLAMA_LLM_LIBRARY: metal\n" +
		"Engine source: default\n" +
		"Engine config: /Users/test/.config/ocelot/engine.env\n" +
		"Metal GPU device: Apple M3 Ultra\n" +
		"Supported quant types: tq1_0, tq2_0, tq3_1s, q4_0, q4_k_m, q8_0, f16\n" +
		"GGML backends: CPU, Metal\n" +
		"llama-server path: /Users/test/.local/share/ocelot/lib/ollama/llama-server\n" +
		"llama-server size: 9167312 bytes\n" +
		"dylib count: 50\n" +
		"MLX variants: mlx_metal_v3\n" +
		"MLX eligible variants: mlx_metal_v3\n" +
		"MLX feasibility: MLX remains non-default; Apple Silicon darwin/arm64 (apple-silicon) will use newest eligible installed variant mlx_metal_v3 for supported safetensors flows. GGUF models still route to puma.cpp / llama-server.\n"
	if out.String() != want {
		t.Fatalf("printEngineInfo() = %q, want %q", out.String(), want)
	}
}

func TestEngineInfoJSONShape(t *testing.T) {
	info := engineInfo{
		LlamaServerVersion:  "9568 (e0c1f9aad)",
		RuntimeGOOS:         "darwin",
		RuntimeGOARCH:       "arm64",
		Platform:            "darwin/arm64",
		Architecture:        "apple-silicon",
		ConfiguredEngine:    "puma",
		EffectiveEngine:     "puma",
		EffectiveLLMLibrary: "metal",
		EngineSource:        "default",
		EngineConfigPath:    "/Users/test/.config/ocelot/engine.env",
		MetalGPUDevice:      "Apple M3 Ultra",
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     "/Users/test/.local/share/ocelot/lib/ollama/llama-server",
		LlamaServerSize:     9167312,
		DylibCount:          50,
		MLXAvailable:        false,
		MLXVariants:         nil,
		MLXEligibleVariants: nil,
		MLXFeasibility:      "MLX remains non-default and no mlx_* runtime libraries are installed for darwin/arm64 (apple-silicon); GGUF models still route to puma.cpp / llama-server.",
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"llama_server_version",
		"runtime_goos",
		"runtime_goarch",
		"platform",
		"architecture",
		"configured_engine",
		"effective_engine",
		"effective_llm_library",
		"engine_source",
		"engine_config_path",
		"metal_gpu_device",
		"supported_quant_types",
		"ggml_backends",
		"llama_server_path",
		"llama_server_size_bytes",
		"dylib_count",
		"mlx_available",
		"mlx_variants",
		"mlx_eligible_variants",
		"mlx_feasibility",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON output missing key %q in %s", key, payload)
		}
	}
}

func TestEngineCurrentJSONShapeIncludesPlatformFields(t *testing.T) {
	current := engineCurrentInfo{
		ConfiguredEngine:     "intel-metal",
		ConfiguredLLMLibrary: "metal",
		EffectiveEngine:      "intel-metal",
		EffectiveLLMLibrary:  "metal",
		Source:               "config",
		ConfigPath:           "/Users/test/.config/ocelot/engine.env",
		ConfigExists:         true,
		RuntimeGOOS:          "darwin",
		RuntimeGOARCH:        "amd64",
		Platform:             "darwin/amd64",
		Architecture:         "intel",
	}

	payload, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"configured_engine",
		"configured_llm_library",
		"effective_engine",
		"effective_llm_library",
		"source",
		"config_path",
		"config_exists",
		"runtime_goos",
		"runtime_goarch",
		"platform",
		"architecture",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON output missing key %q in %s", key, payload)
		}
	}
}

func TestAppleSiliconDefaultResolvesToPumaMetal(t *testing.T) {
	withEnginePlatform(t, "darwin", "arm64")
	unsetEnv(t, "OLLAMA_LLM_LIBRARY")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "default" || current.EffectiveEngine != "puma" || current.EffectiveLLMLibrary != "metal" {
		t.Fatalf("resolveCurrentEngine() = %+v, want puma default on Apple Silicon", current)
	}
	if current.RuntimeGOOS != "darwin" || current.RuntimeGOARCH != "arm64" || current.Architecture != "apple-silicon" {
		t.Fatalf("resolveCurrentEngine() platform = %+v, want darwin/arm64 apple-silicon", current)
	}
}

func TestIntelDefaultResolvesToIntelMetal(t *testing.T) {
	withEnginePlatform(t, "darwin", "amd64")
	unsetEnv(t, "OLLAMA_LLM_LIBRARY")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "default" || current.EffectiveEngine != "intel-metal" || current.EffectiveLLMLibrary != "metal" {
		t.Fatalf("resolveCurrentEngine() = %+v, want intel-metal default on Intel Mac", current)
	}
	if current.RuntimeGOOS != "darwin" || current.RuntimeGOARCH != "amd64" || current.Architecture != "intel" {
		t.Fatalf("resolveCurrentEngine() platform = %+v, want darwin/amd64 intel", current)
	}
}

func TestEngineConfigRoundTrip(t *testing.T) {
	unsetEnv(t, "OLLAMA_LLM_LIBRARY")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)

	selection, err := resolveEngineSelection("cpu")
	if err != nil {
		t.Fatal(err)
	}
	written, err := writeEngineConfig(selection)
	if err != nil {
		t.Fatal(err)
	}
	if written != configPath {
		t.Fatalf("writeEngineConfig path = %q, want %q", written, configPath)
	}

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "config" || current.EffectiveEngine != "cpu" || current.EffectiveLLMLibrary != "cpu" {
		t.Fatalf("resolveCurrentEngine() = %+v, want cpu from config", current)
	}
}

func TestEngineConfigRoundTripIntelTargets(t *testing.T) {
	withEnginePlatform(t, "darwin", "arm64")
	unsetEnv(t, "OLLAMA_LLM_LIBRARY")

	tests := []struct {
		name       string
		wantLLM    string
		wantNotice string
	}{
		{name: "intel-metal", wantLLM: "metal", wantNotice: "cross-target"},
		{name: "intel-cpu", wantLLM: "cpu", wantNotice: "cross-target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "engine.env")
			t.Setenv("OCELOT_ENGINE_CONFIG", configPath)

			selection, err := resolveEngineSelection(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if selection.Name != tt.name || selection.LLMLibrary != tt.wantLLM {
				t.Fatalf("resolveEngineSelection(%q) = %+v, want %s/%s", tt.name, selection, tt.name, tt.wantLLM)
			}
			if !strings.Contains(selection.Warning, tt.wantNotice) {
				t.Fatalf("resolveEngineSelection(%q) warning = %q, want %q", tt.name, selection.Warning, tt.wantNotice)
			}

			if _, err := writeEngineConfig(selection); err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `export OCELOT_ENGINE="`+tt.name+`"`) {
				t.Fatalf("engine config = %q, want OCELOT_ENGINE for %s", data, tt.name)
			}
			if !strings.Contains(string(data), `export OLLAMA_LLM_LIBRARY="`+tt.wantLLM+`"`) {
				t.Fatalf("engine config = %q, want OLLAMA_LLM_LIBRARY for %s", data, tt.wantLLM)
			}

			current, err := resolveCurrentEngine()
			if err != nil {
				t.Fatal(err)
			}
			if current.Source != "config" || current.EffectiveEngine != tt.name || current.EffectiveLLMLibrary != tt.wantLLM {
				t.Fatalf("resolveCurrentEngine() = %+v, want %s from config", current, tt.name)
			}
		})
	}
}

func TestEmptyEngineConfigDefaultsToPuma(t *testing.T) {
	withEnginePlatform(t, "darwin", "arm64")
	unsetEnv(t, "OLLAMA_LLM_LIBRARY")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)
	if err := os.WriteFile(configPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "default" || current.EffectiveEngine != "puma" || current.EffectiveLLMLibrary != "metal" {
		t.Fatalf("resolveCurrentEngine() = %+v, want puma default for empty config", current)
	}
}

func TestEngineConfigAutoUnsetsLLMLibrary(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)

	selection, err := resolveEngineSelection("auto")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeEngineConfig(selection); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "unset OLLAMA_LLM_LIBRARY") {
		t.Fatalf("engine config = %q, want unset OLLAMA_LLM_LIBRARY", data)
	}
}

func TestResolveCurrentEngineEnvOverridesConfig(t *testing.T) {
	withEnginePlatform(t, "darwin", "amd64")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)
	t.Setenv("OLLAMA_LLM_LIBRARY", "cpu_avx2")

	selection, err := resolveEngineSelection("puma")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeEngineConfig(selection); err != nil {
		t.Fatal(err)
	}

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "environment" || current.EffectiveEngine != "intel-cpu" || current.EffectiveLLMLibrary != "cpu_avx2" {
		t.Fatalf("resolveCurrentEngine() = %+v, want intel-cpu cpu_avx2 from environment", current)
	}
}

func TestResolveCurrentEngineRejectsIneligibleMLXEnvVariant(t *testing.T) {
	withEnginePlatform(t, "darwin", "amd64")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)
	t.Setenv("OLLAMA_LLM_LIBRARY", "mlx_metal_v4")

	_, err := resolveCurrentEngine()
	if err == nil {
		t.Fatal("resolveCurrentEngine() succeeded with Intel-ineligible MLX env variant")
	}
	if !strings.Contains(err.Error(), "not eligible") || !strings.Contains(err.Error(), "mlx_metal_v3") {
		t.Fatalf("resolveCurrentEngine() error = %v, want MLX platform eligibility failure", err)
	}
}

func TestResolveCurrentEngineAcceptsEligibleMLXEnvVariant(t *testing.T) {
	withEnginePlatform(t, "darwin", "amd64")
	configPath := filepath.Join(t.TempDir(), "engine.env")
	t.Setenv("OCELOT_ENGINE_CONFIG", configPath)
	t.Setenv("OLLAMA_LLM_LIBRARY", "mlx_metal_v3")

	current, err := resolveCurrentEngine()
	if err != nil {
		t.Fatal(err)
	}
	if current.Source != "environment" || current.EffectiveEngine != "mlx" || current.EffectiveLLMLibrary != "mlx_metal_v3" {
		t.Fatalf("resolveCurrentEngine() = %+v, want eligible MLX env override", current)
	}
}

func TestEngineDetectMLXVariantsNewestFirst(t *testing.T) {
	libDir := t.TempDir()
	for _, dir := range []string{"mlx_metal_v3", "mlx_metal_v10", "mlx_metal_v4", "other"} {
		if err := os.Mkdir(filepath.Join(libDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := detectMLXVariants(libDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mlx_metal_v10", "mlx_metal_v4", "mlx_metal_v3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("detectMLXVariants() = %v, want %v", got, want)
	}
}

func TestResolveMLXSelectionRequiresInstalledRuntime(t *testing.T) {
	withEnginePlatform(t, "darwin", "arm64")
	home := t.TempDir()
	t.Setenv("OCELOT_HOME", home)
	libDir := filepath.Join(home, "lib", "ollama")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveEngineSelection("mlx"); err == nil {
		t.Fatal("resolveEngineSelection(\"mlx\") succeeded without installed MLX runtime")
	}

	if err := os.Mkdir(filepath.Join(libDir, "mlx_metal_v3"), 0o755); err != nil {
		t.Fatal(err)
	}
	selection, err := resolveEngineSelection("mlx")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "mlx" || selection.LLMLibrary != "mlx_metal_v3" {
		t.Fatalf("resolveEngineSelection(\"mlx\") = %+v, want mlx_metal_v3", selection)
	}
}

func TestResolveMLXSelectionIntelUsesV3AndIgnoresV4(t *testing.T) {
	withEnginePlatform(t, "darwin", "amd64")
	home := t.TempDir()
	t.Setenv("OCELOT_HOME", home)
	libDir := filepath.Join(home, "lib", "ollama")
	if err := os.MkdirAll(filepath.Join(libDir, "mlx_metal_v4"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveEngineSelection("mlx"); err == nil || !strings.Contains(err.Error(), "mlx_metal_v3") {
		t.Fatalf("resolveEngineSelection(\"mlx\") with only v4 error = %v, want clear v3 requirement", err)
	}
	if _, err := resolveEngineSelection("mlx_metal_v4"); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("resolveEngineSelection(\"mlx_metal_v4\") error = %v, want not eligible", err)
	}

	if err := os.Mkdir(filepath.Join(libDir, "mlx_metal_v3"), 0o755); err != nil {
		t.Fatal(err)
	}
	selection, err := resolveEngineSelection("mlx")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "mlx" || selection.LLMLibrary != "mlx_metal_v3" {
		t.Fatalf("resolveEngineSelection(\"mlx\") = %+v, want Intel v3 only", selection)
	}

	report, err := collectEngineMLXReport()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Available || strings.Join(report.EligibleVariants, ",") != "mlx_metal_v3" {
		t.Fatalf("collectEngineMLXReport() = %+v, want only mlx_metal_v3 eligible", report)
	}
}

func TestResolveMLXSelectionAppleSiliconPrefersNewestVariant(t *testing.T) {
	withEnginePlatform(t, "darwin", "arm64")
	home := t.TempDir()
	t.Setenv("OCELOT_HOME", home)
	libDir := filepath.Join(home, "lib", "ollama")
	for _, dir := range []string{"mlx_metal_v3", "mlx_metal_v4"} {
		if err := os.MkdirAll(filepath.Join(libDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	selection, err := resolveEngineSelection("mlx")
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "mlx" || selection.LLMLibrary != "mlx_metal_v4" {
		t.Fatalf("resolveEngineSelection(\"mlx\") = %+v, want newest Apple Silicon variant mlx_metal_v4", selection)
	}
}

func withEnginePlatform(t *testing.T, goos, goarch string) {
	t.Helper()
	oldGOOS := engineRuntimeGOOS
	oldGOARCH := engineRuntimeGOARCH
	engineRuntimeGOOS = func() string { return goos }
	engineRuntimeGOARCH = func() string { return goarch }
	t.Cleanup(func() {
		engineRuntimeGOOS = oldGOOS
		engineRuntimeGOARCH = oldGOARCH
	})
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
