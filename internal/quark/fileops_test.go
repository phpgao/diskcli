package quark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/phpgao/diskcli/internal/provider"
)

func TestList_Pagination(t *testing.T) {
	f := &FakeDoer{}
	// Page 1 has 50 entries (full page); page 2 has 30 (short page ends
	// pagination).
	page1 := makeListResponse(50, 1)
	page2 := makeListResponse(30, 51)
	f.Register("GET", "_page=1", 200, page1)
	f.Register("GET", "_page=2", 200, page2)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	items, err := c.listChildren(context.Background(), "0", 50)
	if err != nil {
		t.Fatalf("listChildren failed: %v", err)
	}
	if len(items) != 80 {
		t.Errorf("total count = %d, want 80", len(items))
	}
	// Verify pagination parameters.
	if !strings.Contains(f.Calls[0].URL, "_page=1") {
		t.Errorf("first request should use _page=1, got: %s", f.Calls[0].URL)
	}
	if !strings.Contains(f.Calls[1].URL, "_page=2") {
		t.Errorf("second request should use _page=2, got: %s", f.Calls[1].URL)
	}
}

func TestList_EmptyDir(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/1/clouddrive/file/sort", 200, `{"code":0,"status":200,"data":{"list":[]}}`)
	c, _ := NewClient("__pus=abc", WithDoer(f))
	items, err := c.listChildren(context.Background(), "0", 50)
	if err != nil {
		t.Fatalf("listChildren failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("empty directory should return 0 entries, got %d", len(items))
	}
}

func TestGetFileByPath_Root(t *testing.T) {
	c, _ := NewClient("__pus=abc")
	it, err := c.statFile(context.Background(), "/")
	if err != nil {
		t.Fatalf("root lookup failed: %v", err)
	}
	if it.Fid != rootFid {
		t.Errorf("root fid = %s, want %s", it.Fid, rootFid)
	}
	if !it.IsDirectory() {
		t.Error("root should be a directory")
	}
}

func TestGetFileByPath_NotFound(t *testing.T) {
	f := &FakeDoer{}
	// Root directory listing returns empty.
	f.Register("GET", "/1/clouddrive/file/sort", 200, `{"code":0,"status":200,"data":{"list":[]}}`)
	c, _ := NewClient("__pus=abc", WithDoer(f))
	_, err := c.statFile(context.Background(), "/missing.txt")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetFileByPath_Found(t *testing.T) {
	f := &FakeDoer{}
	listResp := `{"code":0,"status":200,"data":{"list":[
		{"fid":"fid1","file_name":"test.txt","size":100,"dir":false,"file":true,"updated_at":1700000000000},
		{"fid":"fid2","file_name":"subdir","size":0,"dir":true,"file":false,"updated_at":1700000000000}
	]}}`
	f.Register("GET", "/1/clouddrive/file/sort", 200, listResp)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	it, err := c.statFile(context.Background(), "/test.txt")
	if err != nil {
		t.Fatalf("statFile failed: %v", err)
	}
	if it.Fid != "fid1" {
		t.Errorf("fid = %s, want fid1", it.Fid)
	}
	if it.IsDirectory() {
		t.Error("test.txt should not be a directory")
	}
	if it.Size != 100 {
		t.Errorf("size = %d, want 100", it.Size)
	}
}

func TestMkdir(t *testing.T) {
	f := &FakeDoer{}
	// Root listing (used by pathToFid to resolve parent "/").
	f.Register("GET", "/1/clouddrive/file/sort", 200, `{"code":0,"status":200,"data":{"list":[]}}`)
	// mkdir response.
	f.Register("POST", "/1/clouddrive/file", 200, `{"code":0,"status":200,"data":{"fid":"newfid"}}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	client := &Adapter{client: c}
	err := client.Mkdir(context.Background(), "/newdir")
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	// Verify the create-folder endpoint is called.
	if !strings.Contains(f.Calls[len(f.Calls)-1].URL, "/1/clouddrive/file") {
		t.Errorf("should POST to /1/clouddrive/file, got: %s", f.Calls[len(f.Calls)-1].URL)
	}
}

func TestDelete(t *testing.T) {
	f := &FakeDoer{}
	// File list (used by pathToFid).
	listResp := `{"code":0,"status":200,"data":{"list":[
		{"fid":"fid1","file_name":"a.txt","size":10,"dir":false,"file":true}
	]}}`
	f.Register("GET", "/1/clouddrive/file/sort", 200, listResp)
	f.Register("POST", "/1/clouddrive/file/delete", 200, `{"code":0,"status":200}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	a := &Adapter{client: c}
	err := a.Delete(context.Background(), []string{"/a.txt"})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// Verify the delete endpoint was hit.
	found := false
	for _, call := range f.Calls {
		if call.Method == "POST" && strings.Contains(call.URL, "/file/delete") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should call /file/delete")
	}
}

func TestRename(t *testing.T) {
	f := &FakeDoer{}
	listResp := `{"code":0,"status":200,"data":{"list":[
		{"fid":"fid1","file_name":"old.txt","size":10,"dir":false,"file":true}
	]}}`
	f.Register("GET", "/1/clouddrive/file/sort", 200, listResp)
	f.Register("POST", "/1/clouddrive/file/rename", 200, `{"code":0,"status":200}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	a := &Adapter{client: c}
	err := a.Rename(context.Background(), "/old.txt", "new.txt")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
}

func TestAdapterList(t *testing.T) {
	f := &FakeDoer{}
	listResp := `{"code":0,"status":200,"data":{"list":[
		{"fid":"fid1","file_name":"file1.txt","size":1024,"dir":false,"file":true,"updated_at":1700000000000,"created_at":1700000000000},
		{"fid":"fid2","file_name":"dir1","size":0,"dir":true,"file":false,"updated_at":1700000000000,"created_at":1700000000000}
	]}}`
	f.Register("GET", "/1/clouddrive/file/sort", 200, listResp)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	a := &Adapter{client: c}
	res, err := a.List(context.Background(), "/", provider.ListOpts{PageSize: 50})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(res.Items))
	}
	if res.Items[0].Name != "file1.txt" {
		t.Errorf("first item name = %s, want file1.txt", res.Items[0].Name)
	}
	if res.Items[0].Path != "/file1.txt" {
		t.Errorf("first item path = %s, want /file1.txt", res.Items[0].Path)
	}
	if res.Items[1].IsDirectory != true {
		t.Errorf("second item should be a directory")
	}
	if res.Items[0].Size != 1024 {
		t.Errorf("size = %d, want 1024", res.Items[0].Size)
	}
}

func TestFileItemIsDirectory(t *testing.T) {
	tests := []struct {
		name string
		item FileItem
		want bool
	}{
		{"dir=true", FileItem{Dir: true}, true},
		{"file=true", FileItem{File: true}, false},
		{"both empty size=0 fid set", FileItem{Fid: "x", Size: 0}, true},
		{"both empty size>0", FileItem{Fid: "x", Size: 100}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.IsDirectory(); got != tt.want {
				t.Errorf("IsDirectory() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "/"},
		{".", "/"},
		{"/", "/"},
		{"/a/b/", "/a/b"},
		{"/a//b", "/a//b"}, // double slashes are not merged; left to the path package
	}
	for _, tt := range tests {
		if got := normalizePath(tt.in); got != tt.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// makeListResponse generates a list response with n file entries.
func makeListResponse(n int, startIdx int) string {
	items := make([]string, n)
	for i := 0; i < n; i++ {
		idx := startIdx + i
		items[i] = fmt.Sprintf(`{"fid":"fid%d","file_name":"file%d.txt","size":100,"dir":false,"file":true}`, idx, idx)
	}
	return fmt.Sprintf(`{"code":0,"status":200,"data":{"list":[%s]}}`, strings.Join(items, ","))
}
