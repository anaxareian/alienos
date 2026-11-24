package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testHash = "b1674191a88ec5cdd733e4240a81803105dc412d6c6708d53ab94fc248f4f553"

func tagMap(t *testing.T, body []byte) map[string]string {
	t.Helper()

	var out struct {
		SHA256 string     `json:"sha256"`
		NIP94  [][]string `json:"nip94"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad json: %v: %s", err, body)
	}

	if out.SHA256 != testHash {
		t.Fatalf("descriptor fields lost: %s", body)
	}

	m := map[string]string{}
	for _, tag := range out.NIP94 {
		m[tag[0]] = tag[1]
	}

	return m
}

func TestBUD08UploadAndMirror(t *testing.T) {
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 640, 480)))
	bud08LoadBlob = func(_ context.Context, _ string) (io.ReadSeeker, error) {
		return bytes.NewReader(pngBuf.Bytes()), nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload" && r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"url": "https://cdn.example.com/" + testHash + ".png", "sha256": testHash,
			"size": 184292, "type": "image/png", "uploaded": 1725909682,
		})
	})
	RegisterBUD08(mux)

	for _, p := range []string{"/upload", "/mirror"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("PUT", p, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
		m := tagMap(t, rec.Body.Bytes())
		for k, want := range map[string]string{
			"url": "https://cdn.example.com/" + testHash + ".png", "m": "image/png",
			"x": testHash, "ox": testHash, "size": "184292", "dim": "640x480",
		} {
			if m[k] != want {
				t.Errorf("%s: tag %s = %q, want %q", p, k, m[k], want)
			}
		}
	}

	// Other methods/paths pass through untouched.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/upload", nil))
	if rec.Code != 404 {
		t.Fatalf("GET /upload should pass through, got %d", rec.Code)
	}
}

func TestBUD08ErrorsPassThrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too big", http.StatusRequestEntityTooLarge)
	})
	RegisterBUD08(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("PUT", "/upload", nil))
	if rec.Code != 413 || !bytes.Contains(rec.Body.Bytes(), []byte("too big")) {
		t.Fatalf("error not passed through: %d %s", rec.Code, rec.Body.String())
	}
}
