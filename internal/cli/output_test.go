package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseOutput(t *testing.T) {
	tests := []struct {
		in      string
		want    OutputFormat
		wantErr bool
	}{
		{"", OutputTable, false},
		{"table", OutputTable, false},
		{"wide", OutputWide, false},
		{"json", OutputJSON, false},
		{"yaml", OutputYAML, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseOutput(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseOutput(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseOutput(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsEncodedFormat(t *testing.T) {
	if !isEncodedFormat(OutputJSON) {
		t.Error("json should be an encoded format")
	}
	if !isEncodedFormat(OutputYAML) {
		t.Error("yaml should be an encoded format")
	}
	if isEncodedFormat(OutputTable) {
		t.Error("table should not be an encoded format")
	}
	if isEncodedFormat(OutputWide) {
		t.Error("wide should not be an encoded format")
	}
}

func TestPrintEncoded_JSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"name": "test", "size": 1024}
	if err := printEncoded(&buf, data, OutputJSON); err != nil {
		t.Fatalf("printEncoded JSON failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"name": "test"`) {
		t.Errorf("JSON output should contain name field, got: %s", out)
	}
	if !strings.Contains(out, `"size": 1024`) {
		t.Errorf("JSON output should contain size field, got: %s", out)
	}
}

func TestPrintEncoded_YAML(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"name": "test", "size": 1024}
	if err := printEncoded(&buf, data, OutputYAML); err != nil {
		t.Fatalf("printEncoded YAML failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "name: test") {
		t.Errorf("YAML output should contain name field, got: %s", out)
	}
	if !strings.Contains(out, "size: 1024") {
		t.Errorf("YAML output should contain size field, got: %s", out)
	}
}

func TestPrintEncoded_TableReturnsNil(t *testing.T) {
	// table/wide are not handled by printEncoded; should return nil with no output.
	var buf bytes.Buffer
	data := map[string]any{"name": "test"}
	if err := printEncoded(&buf, data, OutputTable); err != nil {
		t.Fatalf("printEncoded table should return nil: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("table format should produce no output, got: %s", buf.String())
	}
}
