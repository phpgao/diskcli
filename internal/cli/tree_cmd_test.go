package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/phpgao/diskcli/internal/provider"
)

// memFetch returns a fetch func backed by an in-memory map of path -> items.
func memFetch(files map[string][]*provider.FileItem) func(ctx context.Context, p string) ([]*provider.FileItem, error) {
	return func(_ context.Context, p string) ([]*provider.FileItem, error) {
		return files[p], nil
	}
}

func TestRenderTree(t *testing.T) {
	files := map[string][]*provider.FileItem{
		"/": {
			{Name: "a", Path: "/a", IsDirectory: true},
			{Name: "b.txt", Path: "/b.txt", IsDirectory: false, Size: 200},
		},
		"/a": {
			{Name: "a1.txt", Path: "/a/a1.txt", IsDirectory: false, Size: 100},
		},
	}
	var buf bytes.Buffer
	if err := renderTree(context.Background(), &buf, "/", memFetch(files), treeOptions{}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	want := "/" + "\n" +
		"├── a/" + "\n" +
		"│   └── a1.txt" + "\n" +
		"└── b.txt" + "\n"
	if got := buf.String(); got != want {
		t.Errorf("output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderTreeMaxDepth(t *testing.T) {
	files := map[string][]*provider.FileItem{
		"/":     {{Name: "a", Path: "/a", IsDirectory: true}},
		"/a":    {{Name: "a1", Path: "/a/a1", IsDirectory: true}},
		"/a/a1": {{Name: "deep.txt", Path: "/a/a1/deep.txt"}},
	}
	var buf bytes.Buffer
	if err := renderTree(context.Background(), &buf, "/", memFetch(files), treeOptions{maxDepth: 1}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	out := buf.String()
	// With -L 1 the directory /a is shown but must not be expanded.
	if strings.Contains(out, "a1") {
		t.Errorf("-L 1 should not expand /a, got:\n%s", out)
	}
	if !strings.Contains(out, "└── a/") {
		t.Errorf("expected /a to appear, got:\n%s", out)
	}
}

func TestRenderTreeDirsOnly(t *testing.T) {
	files := map[string][]*provider.FileItem{
		"/": {
			{Name: "a", Path: "/a", IsDirectory: true},
			{Name: "b.txt", Path: "/b.txt", IsDirectory: false},
		},
	}
	var buf bytes.Buffer
	if err := renderTree(context.Background(), &buf, "/", memFetch(files), treeOptions{dirsOnly: true}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if strings.Contains(buf.String(), "b.txt") {
		t.Errorf("-d should omit files, got:\n%s", buf.String())
	}
}

func TestRenderTreeHumanReadable(t *testing.T) {
	files := map[string][]*provider.FileItem{
		"/": {{Name: "big.bin", Path: "/big.bin", IsDirectory: false, Size: 1024}},
	}
	var buf bytes.Buffer
	if err := renderTree(context.Background(), &buf, "/", memFetch(files), treeOptions{humanReadable: true}); err != nil {
		t.Fatalf("renderTree: %v", err)
	}
	if !strings.Contains(buf.String(), "1.00 KB") {
		t.Errorf("-h should append a human-readable size, got:\n%s", buf.String())
	}
}
