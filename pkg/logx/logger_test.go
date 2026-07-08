package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNewDefault(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf})
	if !l.Enabled(context.TODO(), slog.LevelInfo) {
		t.Fatal("Info level should be enabled by default")
	}
	if l.Enabled(context.TODO(), slog.LevelDebug) {
		t.Fatal("Debug level should not be enabled by default")
	}
}

func TestNewVerbose(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Verbose: true, Writer: &buf})
	if !l.Enabled(context.TODO(), slog.LevelDebug) {
		t.Fatal("Debug level should be enabled with Verbose")
	}
}

func TestNewJSON(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{JSON: true, Writer: &buf})
	l.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) {
		t.Fatalf("JSON output should contain msg field, got: %s", out)
	}
	if !strings.Contains(out, `"key":"value"`) {
		t.Fatalf("JSON output should contain attribute, got: %s", out)
	}
}

func TestNewText(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Writer: &buf})
	l.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, `level=INFO`) {
		t.Fatalf("Text output should contain level=INFO, got: %s", out)
	}
	if !strings.Contains(out, `msg=hello`) {
		t.Fatalf("Text output should contain msg=hello, got: %s", out)
	}
}
