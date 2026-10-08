package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func validateOutput(format string) error {
	switch format {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("unknown output format %q: expected table, json or yaml", format)
	}
}

func structuredOutput() bool { return g.Output != "" && g.Output != "table" }

func writeStructuredOutput(cmd *cobra.Command, value any) error {
	var data []byte
	var err error
	switch value := value.(type) {
	case proto.Message:
		data, err = protojson.Marshal(value)
	case []proto.Message:
		items := make([]json.RawMessage, len(value))
		for i, message := range value {
			items[i], err = protojson.Marshal(message)
			if err != nil {
				return fmt.Errorf("encode output item %d: %w", i, err)
			}
		}
		data, err = json.Marshal(items)
	default:
		data, err = json.Marshal(value)
	}
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	var output bytes.Buffer
	switch g.Output {
	case "json":
		if err := json.Indent(&output, data, "", "  "); err != nil {
			return fmt.Errorf("format JSON output: %w", err)
		}
		output.WriteByte('\n')
	case "yaml":
		var document yaml.Node
		if err := yaml.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("format YAML output: %w", err)
		}
		blockStyle(&document)
		encoder := yaml.NewEncoder(&output)
		encoder.SetIndent(2)
		if err := encoder.Encode(&document); err != nil {
			return fmt.Errorf("encode YAML output: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return fmt.Errorf("finish YAML output: %w", err)
		}
	default:
		return validateOutput(g.Output)
	}
	_, err = cmd.OutOrStdout().Write(output.Bytes())
	return err
}

func blockStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		blockStyle(child)
	}
}
