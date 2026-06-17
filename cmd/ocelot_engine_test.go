package cmd

import (
	"bytes"
	"encoding/json"
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
		MetalGPUDevice:      "Apple M3 Ultra",
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     "/Users/test/.local/share/ocelot/lib/ollama/llama-server",
		LlamaServerSize:     9167312,
		DylibCount:          50,
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	printEngineInfo(cmd, info)

	want := "puma.cpp / llama-server version: 9568 (e0c1f9aad)\n" +
		"Metal GPU device: Apple M3 Ultra\n" +
		"Supported quant types: tq1_0, tq2_0, tq3_1s, q4_0, q4_k_m, q8_0, f16\n" +
		"GGML backends: CPU, Metal\n" +
		"llama-server path: /Users/test/.local/share/ocelot/lib/ollama/llama-server\n" +
		"llama-server size: 9167312 bytes\n" +
		"dylib count: 50\n"
	if out.String() != want {
		t.Fatalf("printEngineInfo() = %q, want %q", out.String(), want)
	}
}

func TestEngineInfoJSONShape(t *testing.T) {
	info := engineInfo{
		LlamaServerVersion:  "9568 (e0c1f9aad)",
		MetalGPUDevice:      "Apple M3 Ultra",
		SupportedQuantTypes: supportedEngineQuantTypes(),
		GGMLBackends:        []string{"CPU", "Metal"},
		LlamaServerPath:     "/Users/test/.local/share/ocelot/lib/ollama/llama-server",
		LlamaServerSize:     9167312,
		DylibCount:          50,
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
		"metal_gpu_device",
		"supported_quant_types",
		"ggml_backends",
		"llama_server_path",
		"llama_server_size_bytes",
		"dylib_count",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON output missing key %q in %s", key, payload)
		}
	}
}
