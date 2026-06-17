package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"

	fsggml "github.com/ollama/ollama/fs/ggml"
	"github.com/ollama/ollama/manifest"
	modeltypes "github.com/ollama/ollama/types/model"
)

func TestDeriveAddModelName(t *testing.T) {
	got := deriveAddModelName("/models/Gemma_2B_Instruct.GGUF")
	if got != "gemma-2b-instruct" {
		t.Fatalf("deriveAddModelName() = %q, want %q", got, "gemma-2b-instruct")
	}
}

func TestAddGGUFFileWritesManifest(t *testing.T) {
	modelsDir := t.TempDir()
	t.Setenv("OLLAMA_MODELS", modelsDir)

	ggufPath := writeTestGGUF(t, filepath.Join(t.TempDir(), "Gemma_2B_Instruct.gguf"))
	added, err := addGGUFFile(ggufPath, addOptions{family: "gemma"})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := added.name.DisplayShortest(), "gemma-2b-instruct:latest"; got != want {
		t.Fatalf("added name = %q, want %q", got, want)
	}
	if got, want := added.config.ModelFamily, "gemma"; got != want {
		t.Fatalf("model family = %q, want %q", got, want)
	}
	if got, want := added.config.FileType, "Q4_K_M"; got != want {
		t.Fatalf("file type = %q, want %q", got, want)
	}
	if got, want := added.config.ContextLen, 8192; got != want {
		t.Fatalf("context length = %d, want %d", got, want)
	}
	if got, want := added.config.EmbedLen, 2048; got != want {
		t.Fatalf("embedding length = %d, want %d", got, want)
	}

	blobPath, err := manifest.BlobsPath(added.layer.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("blob missing: %v", err)
	}

	mf, err := manifest.ParseNamedManifest(modeltypes.ParseName("gemma-2b-instruct"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Layers) != 1 || mf.Layers[0].MediaType != ocelotModelMediaType {
		t.Fatalf("manifest layers = %#v", mf.Layers)
	}
	if mf.Config.MediaType != ocelotConfigMediaType {
		t.Fatalf("config media type = %q, want %q", mf.Config.MediaType, ocelotConfigMediaType)
	}
}

func TestAddHandlerScansDirectoryAndUpdatesLLMDynamix(t *testing.T) {
	home := t.TempDir()
	modelsDir := filepath.Join(home, ".ocelot")
	t.Setenv("HOME", home)
	t.Setenv("OLLAMA_MODELS", modelsDir)

	dir := t.TempDir()
	writeTestGGUF(t, filepath.Join(dir, "First_Model.gguf"))
	writeTestGGUF(t, filepath.Join(dir, "nested", "Second_Model.gguf"))

	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"first-model", "second-model"} {
		if _, err := manifest.ParseNamedManifest(modeltypes.ParseName(name)); err != nil {
			t.Fatalf("manifest for %s missing: %v", name, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(home, ".llmdynamix", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg llmDynamixConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.OpenAICompatibility) != 1 {
		t.Fatalf("openai compatibility entries = %d, want 1", len(cfg.OpenAICompatibility))
	}

	got := []string{
		cfg.OpenAICompatibility[0].Models[0].Name,
		cfg.OpenAICompatibility[0].Models[1].Name,
	}
	want := []string{"first-model:latest", "second-model:latest"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("llmdynamix models mismatch (-want +got):\n%s", diff)
	}
	if !strings.Contains(out.String(), "added first-model:latest") || !strings.Contains(out.String(), "added second-model:latest") {
		t.Fatalf("output missing added models: %s", out.String())
	}
}

func TestAddRejectsInvalidGGUF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.gguf")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := addGGUFFile(path, addOptions{}); err == nil {
		t.Fatal("addGGUFFile succeeded for invalid GGUF")
	}
}

func writeTestGGUF(t *testing.T, path string) string {
	t.Helper()
	return writeTestGGUFWithArch(t, path, "gemma2")
}

func writeTestGGUFWithArch(t *testing.T, path string, arch string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tensorData := bytes.Repeat([]byte{1}, 256*416*4)
	tensors := []*fsggml.Tensor{
		{Name: "token_embd.weight", Shape: []uint64{256, 416}, WriterTo: bytes.NewReader(tensorData)},
	}
	if err := fsggml.WriteGGUF(f, fsggml.KV{
		"general.architecture": arch,
		"general.file_type":    fsggml.FileTypeQ4_K_M,
		"general.alignment":    uint32(32),
		"context_length":       uint32(8192),
		"embedding_length":     uint32(2048),
	}, tensors); err != nil {
		t.Fatal(err)
	}
	return path
}
