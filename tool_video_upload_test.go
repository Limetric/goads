package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadYouTubeVideo_PreviewThenApply(t *testing.T) {
	useTempState(t)
	videoPath := filepath.Join(t.TempDir(), "creative.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/resumable/upload/v23/customers/123/youTubeVideoUploads:create" {
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/upload")
			return
		}
		if r.URL.Path == "/upload" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resourceName":"customers/123/youTubeVideoUploads/456"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	args := UploadYouTubeVideoArgs{CustomerID: "123", VideoFile: videoPath, Title: "CMV creative"}
	preview, err := runUploadYouTubeVideo(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Applied || preview.Token == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	args.Confirm = preview.Token
	applied, err := runUploadYouTubeVideo(t.Context(), c, args)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !applied.Applied || len(applied.ResourceNames) != 1 || applied.ResourceNames[0] != "customers/123/youTubeVideoUploads/456" {
		t.Fatalf("unexpected result: %+v", applied)
	}
}

func TestUploadYouTubeVideo_Validation(t *testing.T) {
	if _, err := runUploadYouTubeVideo(t.Context(), nil, UploadYouTubeVideoArgs{}); err == nil {
		t.Fatal("expected missing path error")
	}

	txtPath := filepath.Join(t.TempDir(), "creative.txt")
	if err := os.WriteFile(txtPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runUploadYouTubeVideo(t.Context(), nil, UploadYouTubeVideoArgs{VideoFile: txtPath, Title: "CMV"}); err == nil {
		t.Fatal("expected extension error")
	}

	mp4Path := filepath.Join(t.TempDir(), "creative.mp4")
	if err := os.WriteFile(mp4Path, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runUploadYouTubeVideo(t.Context(), nil, UploadYouTubeVideoArgs{VideoFile: mp4Path}); err == nil {
		t.Fatal("expected missing title error")
	}
}
