package quark

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/phpgao/diskcli/internal/provider"
	"golang.org/x/sync/errgroup"
)

// Wire-format constants used by the OSS multipart upload protocol. These are
// dictated by the OSS V1 signing rules and the server's XML commit schema;
// changing them breaks the request signature or the commit body.
const (
	// ossHTTPDateFormat formats x-oss-date in RFC 1123 GMT (OSS signing requirement).
	ossHTTPDateFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

	// errPartAlreadyExist is the OSS error code returned when a part was
	// already uploaded in a previous attempt; the response carries the stored
	// ETag so the caller can resume without re-uploading that part.
	errPartAlreadyExist = "PartAlreadyExist"

	// ossPartAuthHeadTmpl is the canonical-string head for a part PUT.
	ossPartAuthHeadTmpl = "PUT\n\n%s\n%s\n"
	// ossHashCtxLineTmpl renders the X-Oss-Hash-Ctx canonical header line that
	// is appended to the part auth_meta starting from the second part.
	ossHashCtxLineTmpl = "X-Oss-Hash-Ctx:%s\n"

	// ossURLFmt builds the OSS object URL as https://{bucket}.{host}/{obj_key}.
	ossURLFmt = "https://%s.%s/%s"

	// maxPartRetries caps the per-part retry count before surfacing an error.
	maxPartRetries = 3
	// hashCtxMinPart is the first part number that must carry X-Oss-Hash-Ctx.
	// Part 1 has no prior chain to verify; parts 2..N must include it.
	hashCtxMinPart = 2
)

// PreUploadResult is the pre-upload response.
// Fields match the server response's "data" object directly (no nested wrapper),
// because requestJSON unmarshals only the data field content.
type PreUploadResult struct {
	TaskID    string          `json:"task_id"`
	Bucket    string          `json:"bucket"`
	ObjKey    string          `json:"obj_key"`
	UploadID  string          `json:"upload_id"`
	UploadURL string          `json:"upload_url"`
	AuthInfo  json.RawMessage `json:"auth_info"`
	Callback  json.RawMessage `json:"callback"`
	Finish    bool            `json:"finish"`
	// MetaPartSize/MetaPartThread come from the metadata field (parsed separately).
	MetaPartSize   int64
	MetaPartThread int
}

// PreUploadMetadata is part_size/part_thread from the metadata field.
type PreUploadMetadata struct {
	PartSize   int64 `json:"part_size"`
	PartThread int   `json:"part_thread"`
}

// HashProbeResult is the hash-report response.
type HashProbeResult struct {
	Finish bool `json:"finish"`
}

// AuthKeyResult is the auth_key exchange response.
type AuthKeyResult struct {
	AuthKey string `json:"auth_key"`
}

// osSError is the OSS error response (XML).
type osSError struct {
	XMLName    xml.Name `xml:"Error"`
	Code       string   `xml:"Code"`
	Message    string   `xml:"Message"`
	PartEtag   string   `xml:"PartEtag"`
	PartNumber string   `xml:"PartNumber"`
}

// initiateUpload is the pre-upload step: it reserves a task id and OSS
// multipart upload session on the server.
func (c *Client) initiateUpload(ctx context.Context, fileName, mimeType string, size int64, parentFid string) (*PreUploadResult, error) {
	now := time.Now().UnixMilli()
	body := map[string]any{
		"ccp_hash_update": true,
		"parallel_upload": true,
		"dir_name":        "",
		"file_name":       fileName,
		"format_type":     mimeType,
		"l_created_at":    now,
		"l_updated_at":    now,
		"pdir_fid":        parentFid,
		"size":            size,
	}
	var resp PreUploadResult
	var meta PreUploadMetadata
	sr, _, err := c.request(ctx, http.MethodPost, domains.drivePC, apiPaths.upload.pre, body)
	if err != nil {
		return nil, err
	}
	if !sr.IsSuccess() {
		return nil, &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}
	if len(sr.Data) > 0 {
		if err := json.Unmarshal(sr.Data, &resp); err != nil {
			return nil, fmt.Errorf("decode pre-upload data: %w", err)
		}
	}
	if len(sr.Metadata) > 0 {
		if err := json.Unmarshal(sr.Metadata, &meta); err != nil {
			return nil, fmt.Errorf("decode pre-upload metadata: %w", err)
		}
		resp.MetaPartSize = meta.PartSize
		resp.MetaPartThread = meta.PartThread
	}
	return &resp, nil
}

// probeInstantUpload reports MD5+SHA1 and returns whether the server
// short-circuited the upload because the file is already known.
func (c *Client) probeInstantUpload(ctx context.Context, taskID, md5Hash, sha1Hash string) (bool, error) {
	body := map[string]any{
		"md5":     md5Hash,
		"sha1":    sha1Hash,
		"task_id": taskID,
	}
	var resp HashProbeResult
	if err := c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.upload.hash, body, &resp); err != nil {
		return false, err
	}
	return resp.Finish, nil
}

// getAuthKey exchanges an OSS Authorization (the client assembles the auth_meta
// material and the server issues an auth_key).
func (c *Client) getAuthKey(ctx context.Context, taskID string, authInfo json.RawMessage, authMeta string) (string, error) {
	body := map[string]any{
		"auth_info": authInfo,
		"auth_meta": authMeta,
		"task_id":   taskID,
	}
	var resp AuthKeyResult
	if err := c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.upload.auth, body, &resp); err != nil {
		return "", err
	}
	return resp.AuthKey, nil
}

// finalizeUpload closes the task on the server side after all parts are merged.
func (c *Client) finalizeUpload(ctx context.Context, taskID, objKey string) error {
	body := map[string]any{
		"obj_key": objKey,
		"task_id": taskID,
	}
	return c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.upload.finish, body, nil)
}

// ossPartSignature assembles the canonical string that the server signs for a
// single-part PUT, following the OSS V1 signature format.
//
// From the second part onward the canonical string must carry the X-Oss-Hash-Ctx
// header so the server can validate the hash chain across parts.
func ossPartSignature(mimeType, utcTime, bucket, objKey, uploadID string, partNumber int, hashCtxB64 string) string {
	meta := fmt.Sprintf(ossPartAuthHeadTmpl, mimeType, utcTime)
	if partNumber >= hashCtxMinPart && hashCtxB64 != "" {
		meta += fmt.Sprintf(ossHashCtxLineTmpl, hashCtxB64)
	}
	meta += fmt.Sprintf("x-oss-date:%s\nx-oss-user-agent:%s\n/%s/%s?partNumber=%d&uploadId=%s",
		utcTime, defaultHeaderValues.ossUserAgent, bucket, objKey, partNumber, uploadID)
	return meta
}

// ossCommitSignature builds the auth_meta for commit (POST + application/xml
// + Content-MD5 + x-oss-callback).
func ossCommitSignature(contentMD5, utcTime, callbackB64, bucket, objKey, uploadID string) string {
	return fmt.Sprintf("POST\n%s\napplication/xml\n%s\nx-oss-callback:%s\nx-oss-date:%s\nx-oss-user-agent:%s\n/%s/%s?uploadId=%s",
		contentMD5, utcTime, callbackB64, utcTime, defaultHeaderValues.ossUserAgent, bucket, objKey, uploadID)
}

// ossEndpointURL builds the OSS upload URL (https://{bucket}.{upload_url}/{obj_key}).
func ossEndpointURL(bucket, uploadURL, objKey string) string {
	base := uploadURL
	base = strings.TrimPrefix(base, "https://")
	base = strings.TrimPrefix(base, "http://")
	return fmt.Sprintf(ossURLFmt, bucket, base, objKey)
}

// pushPart PUTs one part to OSS and returns its ETag for the commit step.
//
// A 409 PartAlreadyExist response means this part was uploaded in a previous
// attempt; the server returns the stored ETag in the body, and we reuse it so
// the caller can resume without re-uploading.
func (c *Client) pushPart(ctx context.Context, pre *PreUploadResult, mimeType string, partNumber int, chunkData []byte, hashCtxB64 string) (string, error) {
	utcTime := time.Now().UTC().Format(ossHTTPDateFormat)
	authMeta := ossPartSignature(mimeType, utcTime, pre.Bucket, pre.ObjKey, pre.UploadID, partNumber, hashCtxB64)

	authKey, err := c.getAuthKey(ctx, pre.TaskID, pre.AuthInfo, authMeta)
	if err != nil {
		return "", err
	}

	uploadURL := ossEndpointURL(pre.Bucket, pre.UploadURL, pre.ObjKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(chunkData))
	if err != nil {
		return "", fmt.Errorf("create OSS request failed: %w", err)
	}
	q := req.URL.Query()
	q.Set("partNumber", fmt.Sprintf("%d", partNumber))
	q.Set("uploadId", pre.UploadID)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", authKey)
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("x-oss-date", utcTime)
	req.Header.Set("x-oss-user-agent", defaultHeaderValues.ossUserAgent)
	if partNumber >= hashCtxMinPart && hashCtxB64 != "" {
		req.Header.Set("X-Oss-Hash-Ctx", hashCtxB64)
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return "", fmt.Errorf("OSS PUT failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 409 && strings.Contains(string(bodyBytes), errPartAlreadyExist) {
			var oe osSError
			if err := xml.Unmarshal(bodyBytes, &oe); err == nil && oe.PartEtag != "" {
				return strings.Trim(oe.PartEtag, "\""), nil
			}
		}
		return "", fmt.Errorf("OSS PUT failed status=%d: %s", resp.StatusCode, truncate(string(bodyBytes), 200))
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("OSS response missing ETag")
	}
	return etag, nil
}

// completeMultipart merges parts (CompleteMultipartUpload).
//
// The XML body follows the format used by the aliyun-sdk-js browser SDK:
// an XML declaration, multi-line indented <Part> entries, and ETag values
// wrapped in double quotes inside the <ETag> element. This matches what the
// Quark web client sends and what OSS expects for callback verification.
func (c *Client) completeMultipart(ctx context.Context, pre *PreUploadResult, etags []string) error {
	// Build the XML body.
	var xmlParts []string
	for i, etag := range etags {
		// OSS expects the ETag inside double quotes within the <ETag> element.
		cleanEtag := strings.Trim(etag, "\"")
		xmlParts = append(xmlParts, fmt.Sprintf("  <Part>\n    <PartNumber>%d</PartNumber>\n    <ETag>\"%s\"</ETag>\n  </Part>", i+1, cleanEtag))
	}
	xmlBody := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<CompleteMultipartUpload>\n" + strings.Join(xmlParts, "\n") + "\n</CompleteMultipartUpload>"

	// Content-MD5 = base64(md5(xmlBody))
	md5Sum := md5.Sum([]byte(xmlBody))
	contentMD5 := base64.StdEncoding.EncodeToString(md5Sum[:])

	// callback base64 (callback may be an object or a string; normalize by
	// re-serializing then base64-encoding).
	var callbackB64 string
	if len(pre.Callback) > 0 {
		// Try to parse as an object and re-serialize (normalize).
		var obj any
		if err := json.Unmarshal(pre.Callback, &obj); err == nil {
			if cbBytes, err := json.Marshal(obj); err == nil {
				callbackB64 = base64.StdEncoding.EncodeToString(cbBytes)
			}
		}
		if callbackB64 == "" {
			callbackB64 = base64.StdEncoding.EncodeToString(pre.Callback)
		}
	}

	utcTime := time.Now().UTC().Format(ossHTTPDateFormat)
	authMeta := ossCommitSignature(contentMD5, utcTime, callbackB64, pre.Bucket, pre.ObjKey, pre.UploadID)

	authKey, err := c.getAuthKey(ctx, pre.TaskID, pre.AuthInfo, authMeta)
	if err != nil {
		return err
	}

	uploadURL := ossEndpointURL(pre.Bucket, pre.UploadURL, pre.ObjKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader([]byte(xmlBody)))
	if err != nil {
		return fmt.Errorf("create commit request failed: %w", err)
	}
	q := req.URL.Query()
	q.Set("uploadId", pre.UploadID)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", authKey)
	req.Header.Set("Content-MD5", contentMD5)
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("x-oss-callback", callbackB64)
	req.Header.Set("x-oss-date", utcTime)
	req.Header.Set("x-oss-user-agent", defaultHeaderValues.ossUserAgent)

	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("commit request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("commit failed status=%d: %s", resp.StatusCode, truncate(string(bodyBytes), 200))
	}
	return nil
}

// runPartWorkers uploads parts concurrently.
//
// The producer runs synchronously in the calling goroutine: it reads one part
// at a time, snapshots the SHA1 hash state (for the X-Oss-Hash-Ctx header),
// feeds the part into the hash, then dispatches the part to a worker. An
// errgroup with a bounded limit plays the role of the worker pool: g.Go blocks
// when all workers are busy, providing natural backpressure on the reader.
//
// Two ordering invariants must hold (both are protocol/contract requirements):
//   - The X-Oss-Hash-Ctx for part N describes the chain up to and including
//     part N-1, so the snapshot must be captured BEFORE the current part is
//     folded into the SHA1 state.
//   - Each part's buffer is freshly allocated. Workers run concurrently and a
//     shared buffer would be overwritten by the next read before the slowest
//     worker finished sending it.
//
// When statePath is non-empty, progress is saved after each successful part so
// an interrupted upload can resume. skipParts pre-fills the etag map for parts
// already uploaded (used by resume).
func (c *Client) runPartWorkers(ctx context.Context, pre *PreUploadResult, src io.Reader, size int64, partSize int64, threads int, statePath string, skipParts map[int]string) ([]string, error) {
	if threads < 1 {
		threads = 1
	}
	totalParts := int(size / partSize)
	if size%partSize != 0 {
		totalParts++
	}
	if totalParts == 0 {
		totalParts = 1
	}

	// etags is keyed by part number (1-based) so commit can emit them in
	// ascending order regardless of completion order.
	etags := make(map[int]string, totalParts)
	var etagMu sync.Mutex

	// Pre-fill from resume state.
	for pn, etag := range skipParts {
		if pn >= 1 && pn <= totalParts {
			etags[pn] = etag
		}
	}

	// sha1State accumulates the hash chain across all parts; its snapshot
	// becomes the X-Oss-Hash-Ctx header value for parts >= hashCtxMinPart.
	sha1State := newSha1State()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(threads)

	for i := 1; i <= totalParts; i++ {
		// Each part gets its own buffer. Workers may still be reading from a
		// previous part when we move on, so a shared buffer would be corrupted
		// by the next read.
		buf := make([]byte, partSize)
		n, err := io.ReadFull(src, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("read part %d failed: %w", i, err)
		}
		if n == 0 {
			break
		}
		chunk := buf[:n]

		// Snapshot the hash state before feeding the current part. The header
		// for part N describes the chain up to and including part N-1, so it
		// must be captured prior to the Write call below.
		var hashCtxB64 string
		if i >= hashCtxMinPart {
			if hc, hcErr := snapshotCtx(sha1State.h); hcErr == nil {
				hashCtxB64, _ = encodeCtx(hc)
			}
		}
		// Fold the current part into the hash for the NEXT snapshot.
		sha1State.h.Write(chunk)

		// Skip already-uploaded parts (resume).
		if _, skip := skipParts[i]; skip {
			continue
		}

		partNum := i
		partData := chunk
		partHashCtx := hashCtxB64
		g.Go(func() error {
			etag, perr := c.pushPartWithRetry(gctx, pre, partNum, partData, partHashCtx)
			if perr != nil {
				return fmt.Errorf("part %d upload failed: %w", partNum, perr)
			}
			etagMu.Lock()
			etags[partNum] = etag
			// Save progress when statePath is provided (resume support).
			if statePath != "" {
				completed := make(map[int]string, len(etags))
				for k, v := range etags {
					completed[k] = v
				}
				_ = persistResumeState(statePath, &ResumeState{
					RemotePath:     pre.ObjKey,
					FileSize:       size,
					TaskID:         pre.TaskID,
					Bucket:         pre.Bucket,
					ObjKey:         pre.ObjKey,
					UploadID:       pre.UploadID,
					UploadURL:      pre.UploadURL,
					AuthInfo:       pre.AuthInfo,
					Callback:       pre.Callback,
					PartSize:       partSize,
					PartThread:     threads,
					CompletedParts: completed,
					TotalParts:     totalParts,
				})
			}
			etagMu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Commit expects etags in ascending part-number order.
	result := make([]string, totalParts)
	for i := 1; i <= totalParts; i++ {
		result[i-1] = etags[i]
	}
	return result, nil
}

// pushPartWithRetry wraps pushPart with bounded exponential backoff. A part
// upload may fail transiently (network blips, OSS throttling), so we retry a
// few times before surfacing the error to the caller.
//
// Backoff is delegated to github.com/cenkalti/backoff/v5 — no hand-rolled
// sleep. It provides exponential growth with jitter, honors context
// cancellation, and is capped at maxPartRetries+1 attempts (1 initial try plus
// maxPartRetries retries, matching the previous v4 semantics).
func (c *Client) pushPartWithRetry(ctx context.Context, pre *PreUploadResult, partNumber int, data []byte, hashCtxB64 string) (string, error) {
	eb := backoff.NewExponentialBackOff()
	eb.InitialInterval = 1 * time.Second
	eb.MaxInterval = 8 * time.Second

	return backoff.Retry(ctx,
		func() (string, error) {
			return c.pushPart(ctx, pre, guessContentType(pre.ObjKey), partNumber, data, hashCtxB64)
		},
		backoff.WithBackOff(eb),
		// WithMaxTries counts total attempts; +1 preserves the previous
		// "3 retries" bound (1 initial + 3 retries = 4 attempts).
		backoff.WithMaxTries(uint(maxPartRetries+1)),
	)
}

// guessContentType guesses the MIME type from objKey (simplified; the Quark pre
// request already carries format_type).
//
// A simple suffix-based lookup is used here, primarily for the OSS PUT
// Content-Type.
func guessContentType(objKey string) string {
	lower := strings.ToLower(objKey)
	if strings.HasSuffix(lower, ".txt") {
		return "text/plain"
	}
	if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") {
		return "image/jpeg"
	}
	if strings.HasSuffix(lower, ".png") {
		return "image/png"
	}
	if strings.HasSuffix(lower, ".mp4") {
		return "video/mp4"
	}
	if strings.HasSuffix(lower, ".zip") {
		return "application/zip"
	}
	return "application/octet-stream"
}

// streamHashes returns the MD5 and SHA1 of the supplied reader. The server
// uses these digests to detect whether the file already exists and can be
// served instantly without a full multipart upload.
//
// The reader is consumed in streaming fashion via io.MultiWriter so the file
// is never fully buffered in memory.
func streamHashes(r io.Reader) (md5Hex, sha1Hex string, err error) {
	md5Hash := md5.New()
	sha1Hash := sha1.New()
	if _, err := io.Copy(io.MultiWriter(md5Hash, sha1Hash), r); err != nil {
		return "", "", err
	}
	return fmt.Sprintf("%x", md5Hash.Sum(nil)), fmt.Sprintf("%x", sha1Hash.Sum(nil)), nil
}

// Upload implements provider.Provider.Upload for a single file.
//
// The upload proceeds in stages: a pre-upload request that may short-circuit
// the whole flow if the file is already known to the server; an optional hash
// report that can also short-circuit; and, when neither succeeds, a
// concurrent multipart upload followed by a commit and a final finish call.
//
// Directory uploads are handled by the transfer layer, which calls this
// method recursively.
func (a *Adapter) Upload(ctx context.Context, dstPath string, src provider.UploadSource, opts provider.UploadOpts) (*provider.UploadResult, error) {
	// Resolve the destination path and parent fid.
	dstPath = normalizePath(dstPath)
	parentDir := pathDir(dstPath)
	fileName := pathBase(dstPath)
	parentFid, err := a.client.pathToFid(ctx, parentDir)
	if err != nil {
		return nil, err
	}

	// Obtain the data source (Reader preferred; otherwise open the file).
	reader, size, err := openSource(src)
	if err != nil {
		return nil, err
	}
	if closer, ok := reader.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	mt := guessContentType(fileName)

	// Resume: if --resume is set, try to load a previously saved state.
	var statePath string
	if opts.Resume && src.Path != "" {
		// Only file-mode uploads support resume (not io.Reader streams).
		sp, err := resumeStatePath(dstPath)
		if err == nil {
			if st, loadErr := loadResumeState(sp); loadErr == nil && st.FileSize == size {
				// Resume: skip pre and hash, jump to multipart upload.
				pre := &PreUploadResult{
					TaskID:         st.TaskID,
					Bucket:         st.Bucket,
					ObjKey:         st.ObjKey,
					UploadID:       st.UploadID,
					UploadURL:      st.UploadURL,
					AuthInfo:       st.AuthInfo,
					Callback:       st.Callback,
					MetaPartSize:   st.PartSize,
					MetaPartThread: st.PartThread,
				}
				uploadReader, _, err := openSource(src)
				if err != nil {
					return nil, err
				}
				if closer, ok := uploadReader.(io.Closer); ok {
					defer func() { _ = closer.Close() }()
				}
				threads := opts.Threads
				if threads <= 0 {
					threads = st.PartThread
				}
				if threads <= 0 {
					threads = 3
				}
				etags, err := a.client.runPartWorkers(ctx, pre, uploadReader, size, st.PartSize, threads, sp, st.CompletedParts)
				if err != nil {
					return nil, err
				}
				if err := a.client.completeMultipart(ctx, pre, etags); err != nil {
					return nil, err
				}
				if err := a.client.finalizeUpload(ctx, pre.TaskID, pre.ObjKey); err != nil {
					return nil, err
				}
				discardResumeState(sp)
				return &provider.UploadResult{Path: dstPath, Size: size, FID: pre.TaskID}, nil
			}
		}
		statePath = sp
	}

	// 1. Pre-upload.
	pre, err := a.client.initiateUpload(ctx, fileName, mt, size, parentFid)
	if err != nil {
		return nil, err
	}
	// Save resume state after pre-upload (so we can resume from the multipart step).
	if statePath != "" {
		totalParts := int(size / pre.MetaPartSize)
		if size%pre.MetaPartSize != 0 {
			totalParts++
		}
		if totalParts == 0 {
			totalParts = 1
		}
		_ = persistResumeState(statePath, &ResumeState{
			RemotePath:     dstPath,
			FileSize:       size,
			MimeType:       mt,
			TaskID:         pre.TaskID,
			Bucket:         pre.Bucket,
			ObjKey:         pre.ObjKey,
			UploadID:       pre.UploadID,
			UploadURL:      pre.UploadURL,
			AuthInfo:       pre.AuthInfo,
			Callback:       pre.Callback,
			PartSize:       pre.MetaPartSize,
			PartThread:     pre.MetaPartThread,
			CompletedParts: nil,
			TotalParts:     totalParts,
		})
	}
	if pre.Finish {
		// Instant-upload hit.
		return &provider.UploadResult{Path: dstPath, Size: size, FID: pre.TaskID}, nil
	}

	// 2. Compute MD5+SHA1 and report (instant-upload detection).
	// Note: the file must be re-read after pre (the hash consumes the reader).
	// Simplification: re-open via src.Path when available; otherwise the caller
	// must provide a Seeker for Reader mode.
	hashReader, _, err := openSource(src)
	if err != nil {
		return nil, err
	}
	if closer, ok := hashReader.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	md5Hex, sha1Hex, err := streamHashes(hashReader)
	if err != nil {
		return nil, fmt.Errorf("compute file hashes failed: %w", err)
	}
	finish, err := a.client.probeInstantUpload(ctx, pre.TaskID, md5Hex, sha1Hex)
	if err != nil {
		return nil, err
	}
	if finish {
		return &provider.UploadResult{Path: dstPath, Size: size, FID: pre.TaskID}, nil
	}

	// 3. Re-open the reader for the multipart upload (hash consumed the
	// previous two readers).
	uploadReader, _, err := openSource(src)
	if err != nil {
		return nil, err
	}
	if closer, ok := uploadReader.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	// 4. Concurrent multipart upload.
	threads := opts.Threads
	if threads <= 0 {
		threads = pre.MetaPartThread
	}
	if threads <= 0 {
		threads = 3
	}
	partSize := pre.MetaPartSize
	if partSize <= 0 {
		return nil, fmt.Errorf("server did not return part_size")
	}
	etags, err := a.client.runPartWorkers(ctx, pre, uploadReader, size, partSize, threads, statePath, nil)
	if err != nil {
		return nil, err
	}

	// 5. Commit.
	if err := a.client.completeMultipart(ctx, pre, etags); err != nil {
		return nil, err
	}
	// 6. Finish.
	if err := a.client.finalizeUpload(ctx, pre.TaskID, pre.ObjKey); err != nil {
		return nil, err
	}
	discardResumeState(statePath)
	return &provider.UploadResult{Path: dstPath, Size: size, FID: pre.TaskID}, nil
}

// openSource obtains a reader from an UploadSource.
//
// Reader is preferred; otherwise the file at Path is opened.
func openSource(src provider.UploadSource) (io.Reader, int64, error) {
	if src.Reader != nil {
		return src.Reader, src.Size, nil
	}
	if src.Path == "" {
		return nil, 0, fmt.Errorf("UploadSource provided neither Reader nor Path")
	}
	f, err := osOpen(src.Path)
	if err != nil {
		return nil, 0, fmt.Errorf("open file failed: %w", err)
	}
	size := src.Size
	if size <= 0 {
		fi, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, 0, err
		}
		size = fi.Size()
	}
	return f, size, nil
}

// resumeStatePath returns the resume state filepath: ~/.diskcli/upload/<hash>.json.
func resumeStatePath(remote string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".diskcli", "upload")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	h := md5.Sum([]byte(remote))
	return filepath.Join(dir, fmt.Sprintf("%x.json", h)), nil
}

// ResumeState is persisted to disk after each part upload, enabling resume.
type ResumeState struct {
	RemotePath     string          `json:"remote_path"`
	FileSize       int64           `json:"file_size"`
	MimeType       string          `json:"mime_type"`
	TaskID         string          `json:"task_id"`
	Bucket         string          `json:"bucket"`
	ObjKey         string          `json:"obj_key"`
	UploadID       string          `json:"upload_id"`
	UploadURL      string          `json:"upload_url"`
	AuthInfo       json.RawMessage `json:"auth_info"`
	Callback       json.RawMessage `json:"callback"`
	PartSize       int64           `json:"part_size"`
	PartThread     int             `json:"part_thread"`
	CompletedParts map[int]string  `json:"completed_parts"`
	TotalParts     int             `json:"total_parts"`
}

func persistResumeState(path string, s *ResumeState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return os.WriteFile(path, data, 0o600)
}

func loadResumeState(path string) (*ResumeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s ResumeState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func discardResumeState(path string) {
	_ = os.Remove(path)
}

// pathDir/pathBase/osOpen are thin wrappers over the stdlib.
func pathDir(p string) string           { return path.Dir(p) }
func pathBase(p string) string          { return path.Base(p) }
func osOpen(p string) (*os.File, error) { return os.Open(p) }
