package cmd

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestQuantizeBuildsCommandArgs(t *testing.T) {
	got := buildQuantizeArgs("input.gguf", "output.gguf", quantizeOptions{
		quantType: "q4_k_m",
		threads:   6,
		imatrix:   "importance.dat",
	})
	want := []string{"--threads", "6", "--imatrix", "importance.dat", "input.gguf", "output.gguf", "q4_k_m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildQuantizeArgs() = %#v, want %#v", got, want)
	}
}

func TestQuantizeRejectsUnsupportedType(t *testing.T) {
	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"quantize", "input.gguf", "output.gguf", "--type", "q5_0"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("quantize succeeded with unsupported type")
	} else if !strings.Contains(err.Error(), `unsupported quantization type "q5_0"`) {
		t.Fatalf("error = %q, want unsupported type", err)
	}
}

func TestQuantizeRunsAndPrintsStats(t *testing.T) {
	oldRun := quantizeRunCommand
	defer func() { quantizeRunCommand = oldRun }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	outputPath := filepath.Join(t.TempDir(), "out.gguf")
	quantizeRunCommand = func(_ context.Context, name string, args []string, stdout, stderr io.Writer) error {
		wantName := filepath.Join(home, ".local", "share", "ocelot", "lib", "ollama", "llama-quantize")
		if name != wantName {
			t.Fatalf("binary path = %q, want %q", name, wantName)
		}
		wantArgs := []string{"--threads", "2", "input.gguf", outputPath, "q8_0"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
		if _, err := stdout.Write([]byte("quantizing\n")); err != nil {
			t.Fatal(err)
		}
		if _, err := stderr.Write([]byte("progress\n")); err != nil {
			t.Fatal(err)
		}
		writeTestGGUF(t, outputPath)
		return nil
	}

	cmd := NewCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"quantize", "input.gguf", outputPath, "--type", "q8_0", "--threads", "2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	output := out.String()
	for _, want := range []string{
		"quantizing",
		"progress",
		"output file size:",
		"estimated VRAM:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
