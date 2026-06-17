package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestLLMDynamixRegisterAllListUnregisterAndIdempotency(t *testing.T) {
	home := t.TempDir()
	modelsDir := filepath.Join(home, ".ocelot")
	t.Setenv("HOME", home)
	t.Setenv("OLLAMA_MODELS", modelsDir)

	addModelForLLMDynamixTest(t, "gemma2b", "gemma2")
	addModelForLLMDynamixTest(t, "bge-small", "bert")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	out := executeLLMDynamixCommand(t, "llmdynamix", "--config", configPath, "register", "--all")
	if !strings.Contains(out, "registered 2, updated 0, skipped 0") {
		t.Fatalf("register --all output = %q", out)
	}

	cfg := readLLMDynamixTestConfig(t, configPath)
	got := llmDynamixTestModels(cfg)
	want := map[string]llmDynamixModel{
		"bge-small:latest": {
			Name:    "bge-small:latest",
			BaseURL: llmDynamixOcelotBaseURL,
			Model:   "bge-small:latest",
			Alias:   "bge-small:latest (ocelot)",
			Type:    "embedding",
			APIKey:  "",
		},
		"gemma2b:latest": {
			Name:    "gemma2b:latest",
			BaseURL: llmDynamixOcelotBaseURL,
			Model:   "gemma2b:latest",
			Alias:   "gemma2b:latest (ocelot)",
			Type:    "chat",
			APIKey:  "",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("registered models mismatch (-want +got):\n%s", diff)
	}

	out = executeLLMDynamixCommand(t, "llmdynamix", "--config", configPath, "register", "--all")
	if !strings.Contains(out, "registered 0, updated 0, skipped 2") {
		t.Fatalf("idempotent register --all output = %q", out)
	}
	cfg = readLLMDynamixTestConfig(t, configPath)
	if gotLen := len(llmDynamixTestModels(cfg)); gotLen != 2 {
		t.Fatalf("model count after idempotent register = %d, want 2", gotLen)
	}

	out = executeLLMDynamixCommand(t, "llmdynamix", "--config", configPath, "list")
	for _, wantText := range []string{"bge-small:latest", "embedding", "synced", "gemma2b:latest", "chat"} {
		if !strings.Contains(out, wantText) {
			t.Fatalf("list output missing %q:\n%s", wantText, out)
		}
	}

	out = executeLLMDynamixCommand(t, "llmdynamix", "--config", configPath, "unregister", "gemma2b")
	if !strings.Contains(out, "unregistered gemma2b:latest") {
		t.Fatalf("unregister output = %q", out)
	}
	cfg = readLLMDynamixTestConfig(t, configPath)
	if _, ok := llmDynamixTestModels(cfg)["gemma2b:latest"]; ok {
		t.Fatal("gemma2b still registered after unregister")
	}
}

func TestLLMDynamixRegisterUpdatesExistingEndpointAndType(t *testing.T) {
	home := t.TempDir()
	modelsDir := filepath.Join(home, ".ocelot")
	t.Setenv("HOME", home)
	t.Setenv("OLLAMA_MODELS", modelsDir)
	addModelForLLMDynamixTest(t, "bge-small", "bert")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := []byte("openai-compatibility:\n  - base-url: http://127.0.0.1:9999/v1\n    models:\n      - name: bge-small:latest\n        model: bge-small:latest\n        type: chat\n        api_key: old\n        base_url: http://127.0.0.1:9999/v1\n    name: ocelot (puma.cpp)\n")
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}

	out := executeLLMDynamixCommand(t, "llmdynamix", "--config", configPath, "register", "bge-small")
	if !strings.Contains(out, "registered 0, updated 1, skipped 0") {
		t.Fatalf("register output = %q", out)
	}

	cfg := readLLMDynamixTestConfig(t, configPath)
	entry := cfg.OpenAICompatibility[0]
	if got := entry.BaseURL; got != llmDynamixOcelotBaseURL {
		t.Fatalf("provider base-url = %q, want %q", got, llmDynamixOcelotBaseURL)
	}
	model := llmDynamixTestModels(cfg)["bge-small:latest"]
	if got := model.Type; got != "embedding" {
		t.Fatalf("model type = %q, want embedding", got)
	}
	if got := model.APIKey; got != "" {
		t.Fatalf("api_key = %q, want empty", got)
	}
}

func executeLLMDynamixCommand(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func addModelForLLMDynamixTest(t *testing.T, name string, arch string) {
	t.Helper()
	ggufPath := writeTestGGUFWithArch(t, filepath.Join(t.TempDir(), name+".gguf"), arch)
	if _, err := addGGUFFile(ggufPath, addOptions{name: name, noLLMDynamix: true}); err != nil {
		t.Fatal(err)
	}
}

func readLLMDynamixTestConfig(t *testing.T, path string) llmDynamixConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg llmDynamixConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func llmDynamixTestModels(cfg llmDynamixConfig) map[string]llmDynamixModel {
	models := map[string]llmDynamixModel{}
	for _, provider := range cfg.OpenAICompatibility {
		if provider.BaseURL != llmDynamixOcelotBaseURL && !strings.Contains(strings.ToLower(provider.Name), "ocelot") {
			continue
		}
		for _, model := range provider.Models {
			key := model.Model
			if key == "" {
				key = model.Name
			}
			models[key] = model
		}
	}
	return models
}
