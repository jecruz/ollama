package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var engineInfoRunCommand = runEngineInfoCommand

type engineInfo struct {
	LlamaServerVersion  string   `json:"llama_server_version"`
	MetalGPUDevice      string   `json:"metal_gpu_device"`
	SupportedQuantTypes []string `json:"supported_quant_types"`
	GGMLBackends        []string `json:"ggml_backends"`
	LlamaServerPath     string   `json:"llama_server_path"`
	LlamaServerSize     int64    `json:"llama_server_size_bytes"`
	DylibCount          int      `json:"dylib_count"`
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

	cmd.AddCommand(infoCmd)
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

	return engineInfo{
		LlamaServerVersion:  parseLlamaServerVersion(versionOutput),
		MetalGPUDevice:      parseChipsetModel(gpuOutput),
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     serverPath,
		LlamaServerSize:     stat.Size(),
		DylibCount:          len(dylibs),
	}, nil
}

func ocelotEngineLibDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "ocelot", "lib", "ollama"), nil
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

func supportedEngineQuantTypes() []string {
	return []string{"tq1_0", "tq2_0", "tq3_1s", "q4_0", "q4_k_m", "q8_0", "f16"}
}

func printEngineInfo(cmd *cobra.Command, info engineInfo) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "puma.cpp / llama-server version: %s\n", info.LlamaServerVersion)
	fmt.Fprintf(out, "Metal GPU device: %s\n", info.MetalGPUDevice)
	fmt.Fprintf(out, "Supported quant types: %s\n", strings.Join(info.SupportedQuantTypes, ", "))
	fmt.Fprintf(out, "GGML backends: %s\n", strings.Join(info.GGMLBackends, ", "))
	fmt.Fprintf(out, "llama-server path: %s\n", info.LlamaServerPath)
	fmt.Fprintf(out, "llama-server size: %s bytes\n", strconv.FormatInt(info.LlamaServerSize, 10))
	fmt.Fprintf(out, "dylib count: %d\n", info.DylibCount)
}
