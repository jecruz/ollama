package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollama/ollama/format"
	fsggml "github.com/ollama/ollama/fs/ggml"
)

func TestInfoHandlerShowsGGUFMetadata(t *testing.T) {
	modelsDir := t.TempDir()
	t.Setenv("OLLAMA_MODELS", modelsDir)

	ggufPath := writeTestGGUF(t, filepath.Join(t.TempDir(), "Gemma_2B_Instruct.gguf"))
	added, err := addGGUFFile(ggufPath, addOptions{family: "gemma"})
	if err != nil {
		t.Fatal(err)
	}

	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"info", added.name.DisplayShortest()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, want := range []string{
		"name",
		"gemma-2b-instruct:latest",
		"architecture",
		"gemma2",
		"quantization",
		"Q4_K_M",
		"parameters",
		"106K",
		"context length",
		"8192",
		"embedding dimension",
		"2048",
		"VRAM estimate",
		"blob path",
		"file size",
		"registered",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestInfoHandlerJSON(t *testing.T) {
	modelsDir := t.TempDir()
	t.Setenv("OLLAMA_MODELS", modelsDir)

	ggufPath := writeTestGGUF(t, filepath.Join(t.TempDir(), "BGE-Small.gguf"))
	added, err := addGGUFFile(ggufPath, addOptions{name: "bge-small"})
	if err != nil {
		t.Fatal(err)
	}

	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"info", added.name.DisplayShortest(), "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var got infoModel
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.Name != "bge-small:latest" {
		t.Fatalf("name = %q, want %q", got.Name, "bge-small:latest")
	}
	if got.Architecture != "gemma2" {
		t.Fatalf("architecture = %q, want %q", got.Architecture, "gemma2")
	}
	if got.QuantizationType != "Q4_K_M" {
		t.Fatalf("quantization = %q, want %q", got.QuantizationType, "Q4_K_M")
	}
	if got.ParameterCount != 106496 {
		t.Fatalf("parameter count = %d, want 106496", got.ParameterCount)
	}
	if got.ContextLength != 8192 {
		t.Fatalf("context length = %d, want 8192", got.ContextLength)
	}
	if got.EmbeddingDim != 2048 {
		t.Fatalf("embedding dim = %d, want 2048", got.EmbeddingDim)
	}
	if got.VRAMEstimateBytes != estimateVRAMBytes(106496, fsggml.FileTypeQ4_K_M) {
		t.Fatalf("vram estimate bytes = %d, want %d", got.VRAMEstimateBytes, estimateVRAMBytes(106496, fsggml.FileTypeQ4_K_M))
	}
	if got.VRAMEstimate != format.HumanBytes(got.VRAMEstimateBytes) {
		t.Fatalf("vram estimate = %q, want %q", got.VRAMEstimate, format.HumanBytes(got.VRAMEstimateBytes))
	}
	if _, err := os.Stat(got.BlobPath); err != nil {
		t.Fatalf("blob path missing: %v", err)
	}
	if got.FileSizeBytes == 0 || got.FileSize == "" {
		t.Fatalf("file size not populated: %#v", got)
	}
	if got.RegistrationDate == "" {
		t.Fatalf("registration date not populated: %#v", got)
	}
}

func TestInferQuantizationFromName(t *testing.T) {
	for _, tt := range []struct {
		path string
		want string
	}{
		{"/models/gemma-2b-it-Q4_K_M.gguf", "Q4_K_M"},
		{"/models/bge-small.f16.gguf", "F16"},
		{"/models/model-without-quant.gguf", "unknown"},
	} {
		if got := inferQuantizationFromName(tt.path); got != tt.want {
			t.Fatalf("inferQuantizationFromName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
