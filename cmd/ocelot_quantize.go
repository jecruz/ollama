package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ollama/ollama/format"
	fsggml "github.com/ollama/ollama/fs/ggml"
)

var quantizeRunCommand = runQuantizeCommand

type quantizeOptions struct {
	quantType       string
	threads         int
	imatrix         string
	allowRequantize bool
	register        bool
}

type quantizedModelStats struct {
	fileSize  int64
	vramBytes int64
}

func quantizeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quantize INPUT.gguf OUTPUT.gguf --type QUANT",
		Short: "Quantize a GGUF model with the local Ocelot engine",
		Args:  cobra.ExactArgs(2),
		RunE:  quantizeHandler,
	}

	cmd.Flags().String("type", "", "Quantization type: q4_0, q4_k_m, q8_0, tq1_0, tq2_0, tq3_1s, f16")
	cmd.Flags().Int("threads", 0, "Number of quantization threads")
	cmd.Flags().String("imatrix", "", "Importance matrix file")
	cmd.Flags().Bool("allow-requantize", false, "Allow requantizing tensors that have already been quantized (may reduce quality)")
	cmd.Flags().Bool("register", false, "Register the output GGUF after successful quantization")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func quantizeHandler(cmd *cobra.Command, args []string) error {
	opts := quantizeOptions{
		quantType:       strings.TrimSpace(cmd.Flags().Lookup("type").Value.String()),
		threads:         mustGetIntFlag(cmd, "threads"),
		imatrix:         strings.TrimSpace(cmd.Flags().Lookup("imatrix").Value.String()),
		allowRequantize: mustGetBoolFlag(cmd, "allow-requantize"),
		register:        mustGetBoolFlag(cmd, "register"),
	}
	if err := validateQuantizeType(opts.quantType); err != nil {
		return err
	}

	libDir, err := ocelotEngineLibDir()
	if err != nil {
		return err
	}
	binaryPath := filepath.Join(libDir, "llama-quantize")
	if err := quantizeRunCommand(cmd.Context(), binaryPath, buildQuantizeArgs(args[0], args[1], opts), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
		return err
	}

	stats, err := loadQuantizedModelStats(args[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "output file size: %s\n", format.HumanBytes(stats.fileSize))
	fmt.Fprintf(cmd.OutOrStdout(), "estimated VRAM: %s\n", format.HumanBytes(stats.vramBytes))

	if opts.register {
		added, err := addGGUFFile(args[1], addOptions{})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "added %s from %s\n", added.name.DisplayShortest(), args[1])
		if err := updateLLMDynamixConfig([]string{added.name.DisplayShortest()}); err != nil {
			return err
		}
	}

	return nil
}

func mustGetIntFlag(cmd *cobra.Command, name string) int {
	v, err := cmd.Flags().GetInt(name)
	if err != nil {
		panic(err)
	}
	return v
}

func validateQuantizeType(quantType string) error {
	for _, supported := range supportedEngineQuantTypes() {
		if quantType == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported quantization type %q; supported types: %s", quantType, strings.Join(supportedEngineQuantTypes(), ", "))
}

func buildQuantizeArgs(inputPath, outputPath string, opts quantizeOptions) []string {
	args := make([]string, 0, 8)
	if opts.allowRequantize {
		args = append(args, "--allow-requantize")
	}
	if opts.threads > 0 {
		args = append(args, "--threads", fmt.Sprintf("%d", opts.threads))
	}
	if opts.imatrix != "" {
		args = append(args, "--imatrix", opts.imatrix)
	}
	return append(args, inputPath, outputPath, opts.quantType)
}

func runQuantizeCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return nil
}

func loadQuantizedModelStats(path string) (quantizedModelStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return quantizedModelStats{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return quantizedModelStats{}, err
	}
	if err := validateGGUFMagic(file); err != nil {
		return quantizedModelStats{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return quantizedModelStats{}, err
	}

	decoded, err := fsggml.Decode(file, -1)
	if err != nil {
		return quantizedModelStats{}, fmt.Errorf("parse output GGUF metadata: %w", err)
	}
	kv := decoded.KV()
	return quantizedModelStats{
		fileSize:  info.Size(),
		vramBytes: estimateVRAMBytes(kv.ParameterCount(), kv.FileType()),
	}, nil
}
