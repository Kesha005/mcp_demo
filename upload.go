package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const uploadAddr = "127.0.0.1:8070"

// Allowed file extensions. Add or remove types as you need.
var allowedExt = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".json": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true, ".zip": true,
}


func startUploadServer() {
	token := os.Getenv("UPLOAD_TOKEN")
	if token == "" {
		log.Println("UPLOAD_TOKEN is not set: upload endpoint is disabled")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bearer token check (constant-time compare)
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Cap the request body a little above the file limit
		r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(1<<20))

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file field is required (max 5 MB)", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if header.Size > maxFileSize {
			http.Error(w, "file too large (max 5 MB)", http.StatusRequestEntityTooLarge)
			return
		}

		name, ok := safeName(header.Filename)
		if !ok {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		if !allowedExt[strings.ToLower(filepath.Ext(name))] {
			http.Error(w, "file type not allowed", http.StatusUnsupportedMediaType)
			return
		}

		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		dst, err := os.OpenFile(filepath.Join(filesDir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		n, err := io.Copy(dst, file)
		if err != nil {
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"filename": name, "size": n})
	})

	go func() {
		log.Printf("upload endpoint listening on %s", uploadAddr)
		if err := http.ListenAndServe(uploadAddr, mux); err != nil {
			log.Printf("upload server error: %v", err)
		}
	}()
}