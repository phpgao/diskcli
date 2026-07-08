package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// OutputFormat represents an output format type.
type OutputFormat string

const (
	// OutputTable is a human-friendly table (default).
	OutputTable OutputFormat = "table"
	// OutputWide is a wide table with more fields.
	OutputWide OutputFormat = "wide"
	// OutputJSON is JSON format.
	OutputJSON OutputFormat = "json"
	// OutputYAML is YAML format.
	OutputYAML OutputFormat = "yaml"
)

// parseOutput parses the -o flag value.
//
// Empty string defaults to table. Supports json/yaml/table/wide.
func parseOutput(s string) (OutputFormat, error) {
	switch s {
	case "", "table":
		return OutputTable, nil
	case "wide":
		return OutputWide, nil
	case "json":
		return OutputJSON, nil
	case "yaml":
		return OutputYAML, nil
	default:
		return "", fmt.Errorf("invalid output format: %s (choose json/yaml/table/wide)", s)
	}
}

// printEncoded writes v to w in the given format.
//
// Used by ls/info/share list for unified -o handling.
func printEncoded(w io.Writer, v any, format OutputFormat) error {
	switch format {
	case OutputJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case OutputYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	default:
		// table/wide are handled by each command individually.
		return nil
	}
}

// isEncodedFormat returns true for structured encoding formats (json/yaml).
//
// Commands use this to decide whether to use printEncoded or custom table output.
func isEncodedFormat(format OutputFormat) bool {
	return format == OutputJSON || format == OutputYAML
}
