package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	fsggml "github.com/ollama/ollama/fs/ggml"
	"github.com/ollama/ollama/manifest"
	modelconfig "github.com/ollama/ollama/types/model"
)

const llmDynamixOcelotProviderName = "ocelot (puma.cpp)"

type llmDynamixOcelotModel struct {
	Name string
	Type string
}

type llmDynamixSyncResult struct {
	Added   int
	Updated int
	Skipped int
}

// llmDynamixCommand returns the ocelot llmdynamix command tree.
func llmDynamixCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llmdynamix",
		Short: "Sync Ocelot models to LLM Dynamix",
	}
	cmd.PersistentFlags().String("config", "", "Path to LLM Dynamix config.yaml")

	var registerAll bool
	registerCmd := &cobra.Command{
		Use:   "register [MODEL]",
		Short: "Register Ocelot models in LLM Dynamix",
		Args: func(cmd *cobra.Command, args []string) error {
			if registerAll {
				if len(args) != 0 {
					return errors.New("register --all does not accept a model name")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return llmDynamixRegisterHandler(cmd, args, registerAll)
		},
	}
	registerCmd.Flags().BoolVar(&registerAll, "all", false, "Register all Ocelot models")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List Ocelot model sync status",
		Args:  cobra.ExactArgs(0),
		RunE:  llmDynamixListHandler,
	}

	unregisterCmd := &cobra.Command{
		Use:   "unregister MODEL",
		Short: "Remove an Ocelot model from LLM Dynamix",
		Args:  cobra.ExactArgs(1),
		RunE:  llmDynamixUnregisterHandler,
	}

	cmd.AddCommand(registerCmd, listCmd, unregisterCmd)
	return cmd
}

func llmDynamixRegisterHandler(cmd *cobra.Command, args []string, all bool) error {
	path, err := llmDynamixConfigPath(cmd)
	if err != nil {
		return err
	}

	var models []llmDynamixOcelotModel
	if all {
		models, err = listLLMDynamixOcelotModels()
		if err != nil {
			return err
		}
	} else {
		model, err := loadLLMDynamixOcelotModel(args[0])
		if err != nil {
			return err
		}
		models = []llmDynamixOcelotModel{model}
	}

	result, err := syncLLMDynamixModels(path, models)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "registered %d, updated %d, skipped %d\n", result.Added, result.Updated, result.Skipped)
	return nil
}

func llmDynamixListHandler(cmd *cobra.Command, _ []string) error {
	path, err := llmDynamixConfigPath(cmd)
	if err != nil {
		return err
	}
	models, err := listLLMDynamixOcelotModels()
	if err != nil {
		return err
	}

	root, err := readLLMDynamixConfig(path)
	if err != nil {
		return err
	}
	configured := llmDynamixConfiguredModels(root)

	rows := make([][]string, 0, len(models))
	for _, model := range models {
		status := "not synced"
		if _, ok := configured[model.Name]; ok {
			status = "synced"
		}
		rows = append(rows, []string{model.Name, model.Type, status})
	}

	table := tablewriter.NewWriter(cmd.OutOrStdout())
	table.SetHeader([]string{"MODEL", "TYPE", "STATUS"})
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("    ")
	table.AppendBulk(rows)
	table.Render()
	return nil
}

func llmDynamixUnregisterHandler(cmd *cobra.Command, args []string) error {
	path, err := llmDynamixConfigPath(cmd)
	if err != nil {
		return err
	}
	name, err := normalizeLLMDynamixModelName(args[0])
	if err != nil {
		return err
	}
	removed, err := removeLLMDynamixModel(path, name)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(cmd.OutOrStdout(), "unregistered %s\n", name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s was not registered\n", name)
	}
	return nil
}

func llmDynamixConfigPath(cmd *cobra.Command) (string, error) {
	if path, err := cmd.Flags().GetString("config"); err == nil && strings.TrimSpace(path) != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".llmdynamix", "config.yaml"), nil
}

func listLLMDynamixOcelotModels() ([]llmDynamixOcelotModel, error) {
	manifests, err := manifest.Manifests(true)
	if err != nil {
		return nil, err
	}

	models := make([]llmDynamixOcelotModel, 0, len(manifests))
	for name, mf := range manifests {
		if _, err := infoModelLayer(mf); err != nil {
			continue
		}
		modelType, err := llmDynamixModelType(mf)
		if err != nil {
			return nil, fmt.Errorf("load %s metadata: %w", name.DisplayShortest(), err)
		}
		models = append(models, llmDynamixOcelotModel{
			Name: name.DisplayShortest(),
			Type: modelType,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].Name < models[j].Name
	})
	return models, nil
}

func loadLLMDynamixOcelotModel(nameText string) (llmDynamixOcelotModel, error) {
	name := modelconfig.ParseName(strings.TrimSpace(nameText))
	if !name.IsValid() {
		return llmDynamixOcelotModel{}, fmt.Errorf("invalid model name: %s", nameText)
	}
	mf, err := manifest.ParseNamedManifest(name)
	if err != nil {
		return llmDynamixOcelotModel{}, fmt.Errorf("load manifest for %s: %w", name.DisplayShortest(), err)
	}
	if _, err := infoModelLayer(mf); err != nil {
		return llmDynamixOcelotModel{}, err
	}
	modelType, err := llmDynamixModelType(mf)
	if err != nil {
		return llmDynamixOcelotModel{}, fmt.Errorf("load %s metadata: %w", name.DisplayShortest(), err)
	}
	return llmDynamixOcelotModel{Name: name.DisplayShortest(), Type: modelType}, nil
}

func llmDynamixModelType(mf *manifest.Manifest) (string, error) {
	layer, err := infoModelLayer(mf)
	if err != nil {
		return "", err
	}
	blobPath, err := manifest.BlobsPath(layer.Digest)
	if err != nil {
		return "", err
	}
	file, err := os.Open(blobPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err := validateGGUFMagic(file); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	decoded, err := fsggml.Decode(file, -1)
	if err != nil {
		return "", fmt.Errorf("parse GGUF metadata: %w", err)
	}
	arch := strings.ToLower(strings.TrimSpace(decoded.KV().Architecture()))
	if arch == "bert" || arch == "nomic-bert" {
		return "embedding", nil
	}
	return "chat", nil
}

func normalizeLLMDynamixModelName(nameText string) (string, error) {
	name := modelconfig.ParseName(strings.TrimSpace(nameText))
	if !name.IsValid() {
		return "", fmt.Errorf("invalid model name: %s", nameText)
	}
	return name.DisplayShortest(), nil
}

func syncLLMDynamixModels(path string, models []llmDynamixOcelotModel) (llmDynamixSyncResult, error) {
	root, err := readLLMDynamixConfig(path)
	if err != nil {
		return llmDynamixSyncResult{}, err
	}
	doc := root.Content[0]
	provider := ensureLLMDynamixOcelotProvider(doc)
	setMappingValue(provider, "base-url", scalarNode(llmDynamixOcelotBaseURL))

	modelSeq := mappingValue(provider, "models")
	if modelSeq == nil || modelSeq.Kind != yaml.SequenceNode {
		modelSeq = &yaml.Node{Kind: yaml.SequenceNode}
		setMappingValue(provider, "models", modelSeq)
	}

	var result llmDynamixSyncResult
	for _, model := range models {
		entry := findLLMDynamixModelEntry(modelSeq, model.Name)
		if entry == nil {
			modelSeq.Content = append(modelSeq.Content, llmDynamixModelNode(model))
			result.Added++
			continue
		}
		if updateLLMDynamixModelEntry(entry, model) {
			result.Updated++
		} else {
			result.Skipped++
		}
	}

	if err := writeLLMDynamixConfig(path, root); err != nil {
		return llmDynamixSyncResult{}, err
	}
	return result, nil
}

func removeLLMDynamixModel(path string, modelName string) (bool, error) {
	root, err := readLLMDynamixConfig(path)
	if err != nil {
		return false, err
	}
	doc := root.Content[0]
	openAI := mappingValue(doc, "openai-compatibility")
	if openAI == nil || openAI.Kind != yaml.SequenceNode {
		return false, nil
	}
	provider := findLLMDynamixOcelotEntry(openAI)
	if provider == nil {
		return false, nil
	}
	modelSeq := mappingValue(provider, "models")
	if modelSeq == nil || modelSeq.Kind != yaml.SequenceNode {
		return false, nil
	}

	removed := false
	kept := modelSeq.Content[:0]
	for _, entry := range modelSeq.Content {
		if llmDynamixEntryModel(entry) == modelName {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	modelSeq.Content = kept
	if !removed {
		return false, nil
	}
	return true, writeLLMDynamixConfig(path, root)
}

func readLLMDynamixConfig(path string) (yaml.Node, error) {
	root := yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	data, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return yaml.Node{}, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return yaml.Node{}, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	return root, nil
}

func writeLLMDynamixConfig(path string, root yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ensureLLMDynamixOcelotProvider(doc *yaml.Node) *yaml.Node {
	openAI := mappingValue(doc, "openai-compatibility")
	if openAI == nil || openAI.Kind != yaml.SequenceNode {
		openAI = &yaml.Node{Kind: yaml.SequenceNode}
		setMappingValue(doc, "openai-compatibility", openAI)
	}

	provider := findLLMDynamixOcelotEntry(openAI)
	if provider == nil {
		provider = mappingNode(
			"base-url", scalarNode(llmDynamixOcelotBaseURL),
			"models", &yaml.Node{Kind: yaml.SequenceNode},
			"name", scalarNode(llmDynamixOcelotProviderName),
		)
		openAI.Content = append(openAI.Content, provider)
	}
	return provider
}

func findLLMDynamixModelEntry(models *yaml.Node, modelName string) *yaml.Node {
	for _, entry := range models.Content {
		if llmDynamixEntryModel(entry) == modelName {
			return entry
		}
	}
	return nil
}

func llmDynamixEntryModel(entry *yaml.Node) string {
	if model := mappingValue(entry, "model"); model != nil && strings.TrimSpace(model.Value) != "" {
		return strings.TrimSpace(model.Value)
	}
	if name := mappingValue(entry, "name"); name != nil {
		value := strings.TrimSpace(name.Value)
		return strings.TrimSuffix(value, " (ocelot)")
	}
	return ""
}

func llmDynamixConfiguredModels(root yaml.Node) map[string]struct{} {
	configured := map[string]struct{}{}
	if len(root.Content) == 0 {
		return configured
	}
	openAI := mappingValue(root.Content[0], "openai-compatibility")
	if openAI == nil || openAI.Kind != yaml.SequenceNode {
		return configured
	}
	provider := findLLMDynamixOcelotEntry(openAI)
	if provider == nil {
		return configured
	}
	models := mappingValue(provider, "models")
	if models == nil || models.Kind != yaml.SequenceNode {
		return configured
	}
	for _, entry := range models.Content {
		if model := llmDynamixEntryModel(entry); model != "" {
			configured[model] = struct{}{}
		}
	}
	return configured
}

func llmDynamixModelNode(model llmDynamixOcelotModel) *yaml.Node {
	return mappingNode(
		"name", scalarNode(model.Name),
		"base_url", scalarNode(llmDynamixOcelotBaseURL),
		"model", scalarNode(model.Name),
		"alias", scalarNode(model.Name+" (ocelot)"),
		"type", scalarNode(model.Type),
		"api_key", scalarNode(""),
	)
}

func updateLLMDynamixModelEntry(entry *yaml.Node, model llmDynamixOcelotModel) bool {
	changed := false
	updates := map[string]string{
		"name":     model.Name,
		"base_url": llmDynamixOcelotBaseURL,
		"model":    model.Name,
		"alias":    model.Name + " (ocelot)",
		"type":     model.Type,
		"api_key":  "",
	}
	for key, value := range updates {
		node := mappingValue(entry, key)
		if node == nil {
			setMappingValue(entry, key, scalarNode(value))
			changed = true
			continue
		}
		if node.Value != value {
			node.Value = value
			changed = true
		}
	}
	return changed
}
