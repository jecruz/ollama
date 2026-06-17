package cmd

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ollama/ollama/format"
	fsggml "github.com/ollama/ollama/fs/ggml"
	"github.com/ollama/ollama/manifest"
	modelconfig "github.com/ollama/ollama/types/model"
)

const (
	ggufMagic               = 0x46554747
	ocelotModelMediaType    = "application/vnd.ollama.image.model"
	ocelotConfigMediaType   = "application/vnd.docker.container.image.v1+json"
	llmDynamixOcelotBaseURL = "http://127.0.0.1:12435/v1"
)

type addOptions struct {
	name         string
	family       string
	noLLMDynamix bool
}

type addedModel struct {
	name     modelconfig.Name
	filePath string
	layer    manifest.Layer
	config   modelconfig.ConfigV2
}

type llmDynamixConfig struct {
	OpenAICompatibility []llmDynamixOpenAICompatibility `yaml:"openai-compatibility"`
	RemoteManagement    yaml.Node                       `yaml:"remote-management,omitempty"`
}

type llmDynamixOpenAICompatibility struct {
	BaseURL string               `yaml:"base-url"`
	Models  []llmDynamixModel    `yaml:"models"`
	Name    string               `yaml:"name"`
	Extra   map[string]yaml.Node `yaml:",inline"`
}

type llmDynamixModel struct {
	Name    string               `yaml:"name"`
	BaseURL string               `yaml:"base_url,omitempty"`
	Model   string               `yaml:"model,omitempty"`
	Alias   string               `yaml:"alias,omitempty"`
	Type    string               `yaml:"type,omitempty"`
	APIKey  string               `yaml:"api_key"`
	Extra   map[string]yaml.Node `yaml:",inline"`
}

func addHandler(cmd *cobra.Command, args []string) error {
	opts := addOptions{
		name:         strings.TrimSpace(cmd.Flags().Lookup("name").Value.String()),
		family:       strings.TrimSpace(cmd.Flags().Lookup("family").Value.String()),
		noLLMDynamix: mustGetBoolFlag(cmd, "no-llmdynamix"),
	}

	if opts.name != "" && isDirectory(args[0]) {
		return errors.New("--name can only be used when adding a single GGUF file")
	}

	files, err := ggufFilesForAdd(args[0])
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .gguf files found in %s", args[0])
	}

	var added []addedModel
	for _, file := range files {
		model, err := addGGUFFile(file, opts)
		if err != nil {
			return err
		}
		added = append(added, model)
		fmt.Fprintf(cmd.OutOrStdout(), "added %s from %s\n", model.name.DisplayShortest(), file)
	}

	if !opts.noLLMDynamix {
		names := make([]string, 0, len(added))
		for _, model := range added {
			names = append(names, model.name.DisplayShortest())
		}
		if err := updateLLMDynamixConfig(names); err != nil {
			return err
		}
	}

	return nil
}

func mustGetBoolFlag(cmd *cobra.Command, name string) bool {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(err)
	}
	return v
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func ggufFilesForAdd(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".gguf") {
			return nil, fmt.Errorf("%s is not a .gguf file", path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		return []string{abs}, nil
	}

	var files []string
	if err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".gguf") {
			return nil
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		files = append(files, abs)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func addGGUFFile(path string, opts addOptions) (addedModel, error) {
	file, err := os.Open(path)
	if err != nil {
		return addedModel{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return addedModel{}, err
	}

	if err := validateGGUFMagic(file); err != nil {
		return addedModel{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return addedModel{}, err
	}

	decoded, err := fsggml.Decode(file, -1)
	if err != nil {
		return addedModel{}, fmt.Errorf("parse GGUF metadata: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return addedModel{}, err
	}
	layer, err := manifest.NewLayer(file, ocelotModelMediaType)
	if err != nil {
		return addedModel{}, err
	}
	layer.From = path
	layer.Size = info.Size()

	nameText := opts.name
	if nameText == "" {
		nameText = deriveAddModelName(path)
	}
	name := modelconfig.ParseName(nameText)
	if !name.IsValid() {
		return addedModel{}, fmt.Errorf("invalid model name: %s", nameText)
	}

	kv := decoded.KV()
	family := opts.family
	if family == "" {
		family = kv.Architecture()
	}
	config := modelconfig.ConfigV2{
		ModelFormat:   decoded.Name(),
		ModelFamily:   family,
		ModelFamilies: []string{family},
		ModelType:     format.HumanNumber(kv.ParameterCount()),
		FileType:      kv.FileType().String(),
		ContextLen:    int(kv.ContextLength()),
		EmbedLen:      int(kv.EmbeddingLength()),
		Architecture:  "amd64",
		OS:            "linux",
		RootFS: modelconfig.RootFS{
			Type:    "layers",
			DiffIDs: []string{layer.Digest},
		},
	}

	configLayer, err := newAddConfigLayer(config)
	if err != nil {
		return addedModel{}, err
	}
	if err := manifest.WriteManifest(name, configLayer, []manifest.Layer{layer}); err != nil {
		return addedModel{}, err
	}

	return addedModel{name: name, filePath: path, layer: layer, config: config}, nil
}

func validateGGUFMagic(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var magic uint32
	if err := binary.Read(file, binary.LittleEndian, &magic); err != nil {
		return err
	}
	if magic != ggufMagic {
		return fmt.Errorf("invalid GGUF magic: got 0x%08x, want 0x%08x", magic, ggufMagic)
	}
	return nil
}

func deriveAddModelName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func newAddConfigLayer(config modelconfig.ConfigV2) (manifest.Layer, error) {
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(config); err != nil {
		return manifest.Layer{}, err
	}
	return manifest.NewLayer(&b, ocelotConfigMediaType)
}

func updateLLMDynamixConfig(modelNames []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".llmdynamix", "config.yaml")
	models := make([]llmDynamixOcelotModel, 0, len(modelNames))
	for _, name := range modelNames {
		model, err := loadLLMDynamixOcelotModel(name)
		if err != nil {
			return err
		}
		models = append(models, model)
	}
	_, err = syncLLMDynamixModels(path, models)
	return err
}

func findLLMDynamixOcelotEntry(entries *yaml.Node) *yaml.Node {
	for _, entry := range entries.Content {
		if baseURL := mappingValue(entry, "base-url"); baseURL != nil && strings.TrimSpace(baseURL.Value) == llmDynamixOcelotBaseURL {
			return entry
		}
	}
	for _, entry := range entries.Content {
		if name := mappingValue(entry, "name"); name != nil && strings.Contains(strings.ToLower(name.Value), "ocelot") {
			return entry
		}
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func mappingNode(values ...any) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(values); i += 2 {
		key := values[i].(string)
		value := values[i+1].(*yaml.Node)
		node.Content = append(node.Content, scalarNode(key), value)
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
