package quark

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
)

// Layout of Go's sha1 marshaled state, used by snapshotCtx to extract the
// intermediate digest words and processed-byte counter.
//
//	[0:4]    4-byte magic prefix (function/magic identifier)
//	[4:24]   20 bytes of h0..h4, each a big-endian uint32
//	[24:88]  64-byte pending-input buffer (unused by the OSS protocol)
//	[88:96]  8-byte big-endian count of bytes hashed so far
const (
	sha1MagicLen   = 4  // marshaled state prefix length
	sha1StateLen   = 20 // h0..h4 state words (5 * uint32)
	sha1CounterLen = 8  // processed byte-count length (uint64)
)

// HashCtx carries the payload of the X-Oss-Hash-Ctx header, which mirrors the
// internal state of an incremental SHA1 computation as understood by the OSS
// server.
//
// Starting from the second part of a multipart upload, every PUT must include
// this header so the server can verify the chain of parts. Omitting it or
// sending a stale state causes the server to clamp throughput, so the value
// must reflect every byte uploaded so far.
//
// Values are sent as strings to match the OSS V1 signing conventions. The
// field order below is part of the wire contract: the server decodes the
// base64-then-JSON payload positionally, so reordering fields would break
// verification.
type HashCtx struct {
	HashType string `json:"hash_type"` // "sha1"
	H0       string `json:"h0"`
	H1       string `json:"h1"`
	H2       string `json:"h2"`
	H3       string `json:"h3"`
	H4       string `json:"h4"`
	Nl       string `json:"Nl"` // low 32 bits of the processed bit count
	Nh       string `json:"Nh"` // high 32 bits of the processed bit count
	Data     string `json:"data"`
	Num      string `json:"num"`
}

// sha1State is a thin wrapper around an incremental SHA1 hasher whose
// intermediate state can be snapshotted on demand for the X-Oss-Hash-Ctx
// header.
//
// The digest state is obtained via MarshalBinary rather than Sum, because Sum
// finalizes the hash and would prevent further updates. Go's sha1 marshaled
// layout is documented in the package-level constants above.
type sha1State struct {
	h hash.Hash
}

// newSha1State creates a fresh incremental SHA1 hasher.
func newSha1State() *sha1State {
	return &sha1State{h: sha1.New()}
}

// snapshotCtx extracts the current SHA1 intermediate state from hh into a
// HashCtx. The hash is not finalized, so callers may keep feeding bytes after
// this returns.
//
// The bit count is split into Nl (low 32 bits) and Nh (high 32 bits) so files
// beyond a few hundred MB still report a correct length.
func snapshotCtx(hh hash.Hash) (*HashCtx, error) {
	marshaler, ok := hh.(interface {
		MarshalBinary() ([]byte, error)
	})
	if !ok {
		return nil, fmt.Errorf("sha1 does not support MarshalBinary")
	}
	state, err := marshaler.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("extract sha1 state failed: %w", err)
	}
	if len(state) < sha1MagicLen+sha1StateLen+sha1CounterLen {
		return nil, fmt.Errorf("unexpected sha1 state length: %d", len(state))
	}

	// Read the five big-endian state words h0..h4 in a loop.
	var h [5]uint32
	for i := 0; i < 5; i++ {
		h[i] = binary.BigEndian.Uint32(state[sha1MagicLen+i*4:])
	}

	// The trailing 8 bytes encode how many bytes have been hashed so far.
	// OSS expects this as a bit count, so multiply by 8 and split into low/high
	// 32-bit halves.
	totalBytesProcessed := binary.BigEndian.Uint64(state[len(state)-sha1CounterLen:])
	totalBits := totalBytesProcessed * 8
	nl := uint32(totalBits & 0xFFFFFFFF)
	nh := uint32(totalBits >> 32)

	return &HashCtx{
		HashType: "sha1",
		H0:       fmt.Sprintf("%d", h[0]),
		H1:       fmt.Sprintf("%d", h[1]),
		H2:       fmt.Sprintf("%d", h[2]),
		H3:       fmt.Sprintf("%d", h[3]),
		H4:       fmt.Sprintf("%d", h[4]),
		Nl:       fmt.Sprintf("%d", nl),
		Nh:       fmt.Sprintf("%d", nh),
		Data:     "",
		Num:      "0",
	}, nil
}

// encodeCtx marshals a HashCtx to JSON and then base64-encodes it.
//
// This is the final value format for the X-Oss-Hash-Ctx header. A nil ctx
// produces an empty string so callers can pass through optional headers
// without conditional branching.
func encodeCtx(ctx *HashCtx) (string, error) {
	if ctx == nil {
		return "", nil
	}
	jsonData, err := json.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// Compile-time check that sha1State.h satisfies hash.Hash.
var _ hash.Hash = sha1.New()
