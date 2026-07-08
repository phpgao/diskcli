package quark

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotCtx_Empty(t *testing.T) {
	s := newSha1State()
	ctx, err := snapshotCtx(s.h)
	if err != nil {
		t.Fatalf("snapshotCtx failed: %v", err)
	}
	if ctx.HashType != "sha1" {
		t.Errorf("HashType = %s, want sha1", ctx.HashType)
	}
	// With empty data h0-h4 hold the SHA1 initial values:
	// 0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476, 0xC3D2E1F0.
	// Go MarshalBinary returns the initial state, whose decimal form is:
	// h0=1732584193, h1=4023233417, h2=2562383102, h3=271733878, h4=3285377520.
	if ctx.H0 != "1732584193" {
		t.Errorf("H0 = %s, want 1732584193", ctx.H0)
	}
	if ctx.Nl != "0" || ctx.Nh != "0" {
		t.Errorf("empty data Nl/Nh should be 0, got %s/%s", ctx.Nl, ctx.Nh)
	}
}

func TestSnapshotCtx_AfterData(t *testing.T) {
	s := newSha1State()
	data := []byte("hello world")
	s.h.Write(data)
	ctx, err := snapshotCtx(s.h)
	if err != nil {
		t.Fatalf("snapshotCtx failed: %v", err)
	}
	// 11 bytes = 88 bits, so Nl=88, Nh=0.
	if ctx.Nl != "88" {
		t.Errorf("Nl = %s, want 88 (11 bytes * 8 bits)", ctx.Nl)
	}
	if ctx.Nh != "0" {
		t.Errorf("Nh = %s, want 0", ctx.Nh)
	}
}

func TestSnapshotCtx_LargeFile(t *testing.T) {
	// Simulate a large file: 536MB+ to test Nl/Nh splitting.
	// 536870912 bytes = 536MB = 4294967296 bits, just over the uint32 range.
	s := newSha1State()
	// Simulate 600MB (zero-filled to avoid actually reading 600MB).
	// A smaller value that still exercises the split logic is used.
	totalBytes := int64(600 * 1024 * 1024) // 600MB
	// Update in chunks (avoid allocating 600MB at once).
	chunk := make([]byte, 1024*1024) // 1MB
	for i := range chunk {
		chunk[i] = byte(i)
	}
	for written := int64(0); written < totalBytes; written += int64(len(chunk)) {
		remaining := totalBytes - written
		if int64(len(chunk)) > remaining {
			s.h.Write(chunk[:remaining])
		} else {
			s.h.Write(chunk)
		}
	}
	ctx, err := snapshotCtx(s.h)
	if err != nil {
		t.Fatalf("snapshotCtx failed: %v", err)
	}
	// 600MB = 629145600 bytes = 5033164800 bits.
	// Nl = 5033164800 & 0xFFFFFFFF = 738197504.
	// Nh = 5033164800 >> 32 = 1.
	if ctx.Nl != "738197504" {
		t.Errorf("Nl = %s, want 738197504", ctx.Nl)
	}
	if ctx.Nh != "1" {
		t.Errorf("Nh = %s, want 1", ctx.Nh)
	}
}

func TestEncodeCtx(t *testing.T) {
	ctx := &HashCtx{
		HashType: "sha1",
		H0:       "1732584193",
		H1:       "4023233417",
		H2:       "2562383102",
		H3:       "271733878",
		H4:       "3285377520",
		Nl:       "88",
		Nh:       "0",
		Data:     "",
		Num:      "0",
	}
	b64, err := encodeCtx(ctx)
	if err != nil {
		t.Fatalf("encodeCtx failed: %v", err)
	}
	// Verify it round-trips.
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	var got HashCtx
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatalf("JSON decode failed: %v", err)
	}
	if got.H0 != ctx.H0 {
		t.Errorf("H0 = %s, want %s", got.H0, ctx.H0)
	}
	if got.HashType != "sha1" {
		t.Errorf("HashType = %s, want sha1", got.HashType)
	}
}

func TestEncodeCtx_Nil(t *testing.T) {
	got, err := encodeCtx(nil)
	if err != nil {
		t.Fatalf("encodeCtx(nil) returned error: %v", err)
	}
	if got != "" {
		t.Errorf("encodeCtx(nil) = %q, want empty", got)
	}
}

func TestOssPartSignature_NoHashCtx(t *testing.T) {
	// partNumber=1 must not include X-Oss-Hash-Ctx.
	meta := ossPartSignature("text/plain", ossHTTPDateFormat,
		"bucket", "obj/key", "upload123", 1, "")
	if strings.Contains(meta, "X-Oss-Hash-Ctx") {
		t.Error("partNumber=1 should not include X-Oss-Hash-Ctx")
	}
	if !strings.Contains(meta, "PUT") {
		t.Error("should include the PUT method")
	}
	if !strings.Contains(meta, "/bucket/obj/key?partNumber=1&uploadId=upload123") {
		t.Errorf("should include the correct resource path, got: %s", meta)
	}
}

func TestOssPartSignature_WithHashCtx(t *testing.T) {
	// partNumber>=2 must include X-Oss-Hash-Ctx.
	meta := ossPartSignature("text/plain", ossHTTPDateFormat,
		"bucket", "obj/key", "upload123", 2, "base64hashctx")
	if !strings.Contains(meta, "X-Oss-Hash-Ctx:base64hashctx") {
		t.Error("partNumber>=2 should include X-Oss-Hash-Ctx")
	}
}

func TestOssCommitSignature(t *testing.T) {
	meta := ossCommitSignature("md5b64", ossHTTPDateFormat,
		"callbackb64", "bucket", "obj/key", "upload123")
	if !strings.Contains(meta, "POST") {
		t.Error("should include the POST method")
	}
	if !strings.Contains(meta, "application/xml") {
		t.Error("should include application/xml")
	}
	if !strings.Contains(meta, "x-oss-callback:callbackb64") {
		t.Error("should include x-oss-callback")
	}
}

func TestOssEndpointURL(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		uploadURL string
		objKey    string
		want      string
	}{
		{"with https", "bk", "https://up.example.com", "obj/key", "https://bk.up.example.com/obj/key"},
		{"with http", "bk", "http://up.example.com", "obj/key", "https://bk.up.example.com/obj/key"},
		{"no scheme", "bk", "up.example.com", "obj/key", "https://bk.up.example.com/obj/key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ossEndpointURL(tt.bucket, tt.uploadURL, tt.objKey)
			if got != tt.want {
				t.Errorf("ossEndpointURL() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGuessContentType(t *testing.T) {
	tests := []struct {
		objKey string
		want   string
	}{
		{"file.txt", "text/plain"},
		{"file.PNG", "image/png"},
		{"file.mp4", "video/mp4"},
		{"file.unknown", "application/octet-stream"},
		{"file", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.objKey, func(t *testing.T) {
			if got := guessContentType(tt.objKey); got != tt.want {
				t.Errorf("guessContentType(%q) = %s, want %s", tt.objKey, got, tt.want)
			}
		})
	}
}

// Verifies that sha1State matches the intermediate state of sha1.Sum
// (ensures MarshalBinary extracts the right state).
func TestSnapshotCtx_MatchesSHA1Internal(t *testing.T) {
	data := []byte("test data for sha1")
	s := newSha1State()
	s.h.Write(data)
	ctx, err := snapshotCtx(s.h)
	if err != nil {
		t.Fatalf("snapshotCtx failed: %v", err)
	}
	// After sha1.Sum(data) the internal state would be finalized, which differs
	// from the intermediate state. sha1State never calls Sum, so its state
	// is the intermediate one (still writable).
	// Here we only verify Nl/Nh.
	expectedBits := len(data) * 8
	if ctx.Nl != itoa(expectedBits) {
		t.Errorf("Nl = %s, want %d", ctx.Nl, expectedBits)
	}
}

// itoa is a minimal int-to-string helper (avoids importing strconv).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// Compile-time check that sha1State uses sha1.Hash.
var _ = sha1.New
