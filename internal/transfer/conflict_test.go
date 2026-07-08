package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/phpgao/diskcli/internal/provider"
)

// fakeProvider is a test-only provider implementing just Stat/Delete.
type fakeProvider struct {
	exists  map[string]bool
	deletes []string
}

func (f *fakeProvider) Name() string                                                   { return "fake" }
func (f *fakeProvider) Authenticate(ctx context.Context, c provider.Credentials) error { return nil }
func (f *fakeProvider) GetUserInfo(ctx context.Context) (*provider.UserInfo, error)    { return nil, nil }
func (f *fakeProvider) List(ctx context.Context, p string, o provider.ListOpts) (*provider.ListResult, error) {
	return nil, nil
}

func (f *fakeProvider) Stat(ctx context.Context, p string) (*provider.FileItem, error) {
	if f.exists[p] {
		return &provider.FileItem{Name: p}, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeProvider) Mkdir(ctx context.Context, p string) error     { return nil }
func (f *fakeProvider) Move(ctx context.Context, s, d string) error   { return nil }
func (f *fakeProvider) Copy(ctx context.Context, s, d string) error   { return nil }
func (f *fakeProvider) Rename(ctx context.Context, p, n string) error { return nil }
func (f *fakeProvider) Delete(ctx context.Context, paths []string) error {
	f.deletes = append(f.deletes, paths...)
	for _, p := range paths {
		delete(f.exists, p)
	}
	return nil
}

func (f *fakeProvider) Upload(ctx context.Context, d string, s provider.UploadSource, o provider.UploadOpts) (*provider.UploadResult, error) {
	return nil, nil
}

func (f *fakeProvider) Download(ctx context.Context, s string, d io.Writer, o provider.DownloadOpts) error {
	return nil
}
func (f *fakeProvider) GetDownloadURL(ctx context.Context, p string) (string, error) { return "", nil }
func (f *fakeProvider) Search(ctx context.Context, query, scope string) ([]*provider.FileItem, error) {
	return nil, fmt.Errorf("unsupported")
}

func (f *fakeProvider) Unarchive(ctx context.Context, archivePath string, conflictMode int) error {
	return fmt.Errorf("unsupported")
}

func TestResolveConflict_NotExists(t *testing.T) {
	fp := &fakeProvider{exists: map[string]bool{}}
	path, skipped, err := ResolveConflict(context.Background(), fp, "/a.txt", provider.ConflictSkip)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if skipped {
		t.Error("should not skip when not exists")
	}
	if path != "/a.txt" {
		t.Errorf("path = %s, want /a.txt", path)
	}
}

func TestResolveConflict_Skip(t *testing.T) {
	fp := &fakeProvider{exists: map[string]bool{"/a.txt": true}}
	path, skipped, err := ResolveConflict(context.Background(), fp, "/a.txt", provider.ConflictSkip)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if !skipped {
		t.Error("should skip")
	}
	if path != "" {
		t.Errorf("path = %s, want empty", path)
	}
}

func TestResolveConflict_Overwrite(t *testing.T) {
	fp := &fakeProvider{exists: map[string]bool{"/a.txt": true}}
	path, skipped, err := ResolveConflict(context.Background(), fp, "/a.txt", provider.ConflictOverwrite)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if skipped {
		t.Error("should not skip")
	}
	if path != "/a.txt" {
		t.Errorf("path = %s, want /a.txt", path)
	}
	if len(fp.deletes) != 1 || fp.deletes[0] != "/a.txt" {
		t.Errorf("should delete /a.txt, got %v", fp.deletes)
	}
}

func TestResolveConflict_Rename(t *testing.T) {
	fp := &fakeProvider{exists: map[string]bool{"/a.txt": true}}
	path, skipped, err := ResolveConflict(context.Background(), fp, "/a.txt", provider.ConflictRename)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if skipped {
		t.Error("should not skip")
	}
	if path != "/a_1.txt" {
		t.Errorf("path = %s, want /a_1.txt", path)
	}
}

func TestResolveConflict_RenameMultipleExists(t *testing.T) {
	fp := &fakeProvider{exists: map[string]bool{
		"/a.txt":   true,
		"/a_1.txt": true,
		"/a_2.txt": true,
	}}
	path, _, err := ResolveConflict(context.Background(), fp, "/a.txt", provider.ConflictRename)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if path != "/a_3.txt" {
		t.Errorf("path = %s, want /a_3.txt", path)
	}
}

func TestPathDirBase(t *testing.T) {
	tests := []struct {
		in, dir, base string
	}{
		{"/a/b/c.txt", "/a/b", "c.txt"},
		{"/a.txt", "/", "a.txt"},
		{"/dir/", "/", "dir"},
		{"a.txt", "/", "a.txt"},
	}
	for _, tt := range tests {
		if got := pathDir(tt.in); got != tt.dir {
			t.Errorf("pathDir(%q) = %q, want %q", tt.in, got, tt.dir)
		}
		if got := pathBase(tt.in); got != tt.base {
			t.Errorf("pathBase(%q) = %q, want %q", tt.in, got, tt.base)
		}
	}
}

func TestJoinPath(t *testing.T) {
	if got := joinPath("/", "a.txt"); got != "/a.txt" {
		t.Errorf("joinPath(/, a.txt) = %s, want /a.txt", got)
	}
	if got := joinPath("/sub", "a.txt"); got != "/sub/a.txt" {
		t.Errorf("joinPath(/sub, a.txt) = %s, want /sub/a.txt", got)
	}
}

// ensure strings package is used (pathDirImpl references it).
var _ = strings.Contains
