package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	"github.com/ollama/ollama/format"
	fsggml "github.com/ollama/ollama/fs/ggml"
	"github.com/ollama/ollama/manifest"
	modelconfig "github.com/ollama/ollama/types/model"
)

type infoModel struct {
	Name              string `json:"name"`
	Architecture      string `json:"architecture"`
	QuantizationType  string `json:"quantization_type"`
	ParameterCount    uint64 `json:"parameter_count"`
	ContextLength     uint64 `json:"context_length"`
	EmbeddingDim      uint64 `json:"embedding_dimension"`
	VRAMEstimate      string `json:"vram_estimate"`
	VRAMEstimateBytes int64  `json:"vram_estimate_bytes"`
	BlobPath          string `json:"blob_path"`
	FileSize          string `json:"file_size"`
	FileSizeBytes     int64  `json:"file_size_bytes"`
	RegistrationDate  string `json:"registration_date"`
}

func infoHandler(cmd *cobra.Command, args []string) error {
	info, err := loadInfoModel(args[0])
	if err != nil {
		return err
	}

	if mustGetBoolFlag(cmd, "json") {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	renderInfoModel(cmd.OutOrStdout(), info)
	return nil
}

func loadInfoModel(nameText string) (infoModel, error) {
	name := modelconfig.ParseName(strings.TrimSpace(nameText))
	if !name.IsValid() {
		return infoModel{}, fmt.Errorf("invalid model name: %s", nameText)
	}

	mf, err := manifest.ParseNamedManifest(name)
	if err != nil {
		return infoModel{}, fmt.Errorf("load manifest for %s: %w", name.DisplayShortest(), err)
	}

	layer, err := infoModelLayer(mf)
	if err != nil {
		return infoModel{}, err
	}

	blobPath, err := manifest.BlobsPath(layer.Digest)
	if err != nil {
		return infoModel{}, err
	}

	file, err := os.Open(blobPath)
	if err != nil {
		return infoModel{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return infoModel{}, err
	}

	if err := validateGGUFMagic(file); err != nil {
		return infoModel{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return infoModel{}, err
	}

	decoded, err := fsggml.Decode(file, -1)
	if err != nil {
		return infoModel{}, fmt.Errorf("parse GGUF metadata: %w", err)
	}

	kv := decoded.KV()
	quantization := kv.FileType().String()
	if quantization == "unknown" {
		quantization = inferQuantizationFromName(layer.From)
	}

	paramCount := kv.ParameterCount()
	vramBytes := estimateVRAMBytes(paramCount, kv.FileType())

	var registeredAt time.Time
	if fi := mf.FileInfo(); fi != nil {
		registeredAt = fi.ModTime()
	}

	return infoModel{
		Name:              name.DisplayShortest(),
		Architecture:      kv.Architecture(),
		QuantizationType:  quantization,
		ParameterCount:    paramCount,
		ContextLength:     kv.ContextLength(),
		EmbeddingDim:      kv.EmbeddingLength(),
		VRAMEstimate:      format.HumanBytes(vramBytes),
		VRAMEstimateBytes: vramBytes,
		BlobPath:          blobPath,
		FileSize:          format.HumanBytes(info.Size()),
		FileSizeBytes:     info.Size(),
		RegistrationDate:  formatRegistrationDate(registeredAt),
	}, nil
}

func infoModelLayer(mf *manifest.Manifest) (manifest.Layer, error) {
	for _, layer := range mf.Layers {
		if layer.MediaType == ocelotModelMediaType {
			return layer, nil
		}
	}
	return manifest.Layer{}, fmt.Errorf("manifest does not contain an ocelot GGUF model layer")
}

func renderInfoModel(w io.Writer, info infoModel) {
	fmt.Fprintln(w, " ", "Model")
	table := tablewriter.NewWriter(w)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("    ")
	table.AppendBulk([][]string{
		{"", "name", info.Name},
		{"", "architecture", info.Architecture},
		{"", "quantization", info.QuantizationType},
		{"", "parameters", format.HumanNumber(info.ParameterCount)},
		{"", "context length", strconv.FormatUint(info.ContextLength, 10)},
		{"", "embedding dimension", strconv.FormatUint(info.EmbeddingDim, 10)},
		{"", "VRAM estimate", info.VRAMEstimate},
		{"", "blob path", info.BlobPath},
		{"", "file size", info.FileSize},
		{"", "registered", info.RegistrationDate},
	})
	table.Render()
	fmt.Fprintln(w)
}

func estimateVRAMBytes(paramCount uint64, fileType fsggml.FileType) int64 {
	if paramCount == 0 || fileType == fsggml.FileTypeUnknown {
		return 0
	}
	tensorType := fileType.ToTensorType()
	bytesPerWeight := float64(tensorType.TypeSize()) / float64(tensorType.BlockSize())
	return int64(math.Ceil(float64(paramCount) * bytesPerWeight * 1.1))
}

func formatRegistrationDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func inferQuantizationFromName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	re := regexp.MustCompile(`(?i)(?:^|[-_.])((?:I?Q|TQ)\d(?:_[A-Z0-9]+)*|BF16|F16|F32|MXFP4(?:_[A-Z0-9]+)?|NVFP4)(?:$|[-_.])`)
	match := re.FindStringSubmatch(base)
	if len(match) < 2 {
		return "unknown"
	}
	return strings.ToUpper(match[1])
}
