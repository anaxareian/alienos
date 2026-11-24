package main

// BUD-08: Nostr File Metadata Tags.
//
// Adds an optional `nip94` array (NIP-94 file metadata tags) to the blob
// descriptor returned by `PUT /upload` and `PUT /mirror`. The upstream khatru
// blossom handlers are left untouched; this wraps their responses.
//
// Spec: https://github.com/hzrd149/blossom/blob/master/buds/08.md

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// bud08LoadBlob reads a stored blob; used only to read image dimensions.
var bud08LoadBlob func(ctx context.Context, sha256 string) (io.ReadSeeker, error)

// bufferedResponse captures a handler's response so it can be rewritten.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}

	return b.body.Write(p)
}

func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

// RegisterBUD08 wraps the blossom upload and mirror endpoints on mux.
// Call it after blossom.New so the blossom catch-all handler exists.
func RegisterBUD08(mux *http.ServeMux) {
	for _, p := range []string{"/upload", "/mirror"} {
		probe := &http.Request{Method: http.MethodPut, URL: &url.URL{Path: p}, Host: "localhost"}
		inner, _ := mux.Handler(probe)
		mux.Handle("PUT "+p, bud08Wrap(inner))
	}
}

func bud08Wrap(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &bufferedResponse{header: make(http.Header)}
		inner.ServeHTTP(buf, r)

		body := buf.body.Bytes()
		if buf.status == 0 {
			buf.status = http.StatusOK
		}

		if buf.status == http.StatusOK && strings.HasPrefix(buf.header.Get("Content-Type"), "application/json") {
			if out, ok := addNIP94(r.Context(), body); ok {
				body = out
			}
		}

		for k, v := range buf.header {
			w.Header()[k] = v
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(buf.status)
		_, _ = w.Write(body)
	})
}

// addNIP94 decodes a blob descriptor and returns it with a nip94 field added.
// Unknown fields in the descriptor are preserved.
func addNIP94(ctx context.Context, descriptor []byte) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(descriptor, &raw); err != nil {
		return nil, false
	}

	if _, exists := raw["nip94"]; exists {
		return nil, false
	}

	var bd struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		Size   int    `json:"size"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(descriptor, &bd); err != nil || bd.SHA256 == "" {
		return nil, false
	}

	tags := NIP94Tags(ctx, bd.URL, bd.SHA256, bd.Size, bd.Type)

	enc, err := json.Marshal(tags)
	if err != nil {
		return nil, false
	}

	raw["nip94"] = enc

	out, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}

	return append(out, '\n'), true
}

// NIP94Tags builds NIP-94 tags for a stored blob. The server stores blobs
// unchanged, so the original hash (ox) equals the stored hash (x).
func NIP94Tags(ctx context.Context, blobURL, sha256 string, size int, mimetype string) [][]string {
	tags := [][]string{}
	if blobURL != "" {
		tags = append(tags, []string{"url", blobURL})
	}

	if mimetype != "" {
		tags = append(tags, []string{"m", mimetype})
	}

	tags = append(tags,
		[]string{"x", sha256},
		[]string{"ox", sha256},
		[]string{"size", strconv.Itoa(size)},
	)

	if strings.HasPrefix(mimetype, "image/") && bud08LoadBlob != nil {
		if rs, err := bud08LoadBlob(ctx, sha256); err == nil && rs != nil {
			if cfg, _, err := image.DecodeConfig(rs); err == nil && cfg.Width > 0 && cfg.Height > 0 {
				tags = append(tags, []string{"dim", strconv.Itoa(cfg.Width) + "x" + strconv.Itoa(cfg.Height)})
			}

			if c, ok := rs.(io.Closer); ok {
				_ = c.Close()
			}
		}
	}

	return tags
}
