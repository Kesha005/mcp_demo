package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp_demo/store"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	filesDir    = "./uploads"
	maxFileSize = 5 << 20 // 5 MB
	uploadAddr  = "127.0.0.1:8070"

	// Публичный URL, который видит Claude (через nginx)
	// Замени на свой реальный домен!
	publicUploadBase = "https://твой-домен.com/upload"
)

// ---------- Pending uploads ----------

type pendingUpload struct {
	Filename  string
	ExpiresAt time.Time
	Token     string
}

var pending = struct {
	sync.Mutex
	m map[string]*pendingUpload
}{m: make(map[string]*pendingUpload)}

func generateToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func cleanupExpired() {
	for {
		time.Sleep(1 * time.Minute)
		now := time.Now()
		pending.Lock()
		for id, p := range pending.m {
			if now.After(p.ExpiresAt) {
				delete(pending.m, id)
			}
		}
		pending.Unlock()
	}
}

// ---------- Allowed extensions ----------

var allowedExt = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".json": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true, ".zip": true,
	// ".exe": true, // раскомментируй, если реально нужно
}

func main() {
	st := store.New()

	// Запускаем HTTP upload-сервер и очистку просроченных токенов
	go startUploadServer()
	go cleanupExpired()

	s := server.NewMCPServer(
		"users-tasks-server",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// ---------- USER TOOLS ----------

	createUser := mcp.NewTool("create_user",
		mcp.WithDescription("Create a new user"),
		mcp.WithString("name", mcp.Required(), mcp.Description("User's full name")),
		mcp.WithString("email", mcp.Required(), mcp.Description("User's email")),
	)
	s.AddTool(createUser, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		name, _ := args["name"].(string)
		email, _ := args["email"].(string)
		if name == "" || email == "" {
			return mcp.NewToolResultError("name and email are required"), nil
		}
		u := st.CreateUser(name, email)
		return jsonResult(u)
	})

	listUsers := mcp.NewTool("list_users",
		mcp.WithDescription("List all users"),
	)
	s.AddTool(listUsers, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(st.ListUsers())
	})

	getUser := mcp.NewTool("get_user",
		mcp.WithDescription("Get a user by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("User ID")),
	)
	s.AddTool(getUser, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.GetArguments()["id"].(string)
		u, err := st.GetUser(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(u)
	})

	deleteUser := mcp.NewTool("delete_user",
		mcp.WithDescription("Delete a user and their tasks"),
		mcp.WithString("id", mcp.Required(), mcp.Description("User ID")),
	)
	s.AddTool(deleteUser, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.GetArguments()["id"].(string)
		if err := st.DeleteUser(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("user deleted"), nil
	})

	// ---------- TASK TOOLS ----------

	createTask := mcp.NewTool("create_task",
		mcp.WithDescription("Create a task for a user"),
		mcp.WithString("user_id", mcp.Required(), mcp.Description("Owner user ID")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Task title")),
		mcp.WithString("status", mcp.Description("todo|in_progress|done (default: todo)")),
	)
	s.AddTool(createTask, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		userID, _ := args["user_id"].(string)
		title, _ := args["title"].(string)
		status, _ := args["status"].(string)

		if userID == "" || title == "" {
			return mcp.NewToolResultError("user_id and title are required"), nil
		}
		if _, err := st.GetUser(userID); err != nil {
			return mcp.NewToolResultError("user not found"), nil
		}
		t := st.CreateTask(userID, title, status)
		return jsonResult(t)
	})

	listTasks := mcp.NewTool("list_tasks",
		mcp.WithDescription("List tasks, optionally filtered by user"),
		mcp.WithString("user_id", mcp.Description("Filter by user ID (optional)")),
	)
	s.AddTool(listTasks, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		userID, _ := req.GetArguments()["user_id"].(string)
		return jsonResult(st.ListTasks(userID))
	})

	updateTask := mcp.NewTool("update_task_status",
		mcp.WithDescription("Update a task's status"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
		mcp.WithString("status", mcp.Required(), mcp.Description("todo|in_progress|done")),
	)
	s.AddTool(updateTask, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)
		status, _ := args["status"].(string)
		if id == "" || status == "" {
			return mcp.NewToolResultError("id and status are required"), nil
		}
		t, err := st.UpdateTaskStatus(id, status)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(t)
	})

	deleteTask := mcp.NewTool("delete_task",
		mcp.WithDescription("Delete a task"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
	)
	s.AddTool(deleteTask, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.GetArguments()["id"].(string)
		if err := st.DeleteTask(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("task deleted"), nil
	})



	listFiles := mcp.NewTool("list_files",
		mcp.WithDescription("List files saved on the server"),
	)
	s.AddTool(listFiles, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out := []map[string]any{}
		entries, err := os.ReadDir(filesDir)
		if err != nil && !os.IsNotExist(err) {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, map[string]any{
				"filename": e.Name(),
				"size":     info.Size(),
				"saved_at": info.ModTime().UTC().Format(time.RFC3339),
			})
		}
		return jsonResult(out)
	})

	getFile := mcp.NewTool("get_file",
		mcp.WithDescription("Read a saved file as text"),
		mcp.WithString("filename", mcp.Required(), mcp.Description("File name")),
	)
	s.AddTool(getFile, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, _ := req.GetArguments()["filename"].(string)
		name, ok := safeName(filename)
		if !ok {
			return mcp.NewToolResultError("invalid filename"), nil
		}
		data, err := os.ReadFile(filepath.Join(filesDir, name))
		if err != nil {
			return mcp.NewToolResultError("file not found"), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})

	deleteFile := mcp.NewTool("delete_file",
		mcp.WithDescription("Delete a saved file"),
		mcp.WithString("filename", mcp.Required(), mcp.Description("File name")),
	)
	s.AddTool(deleteFile, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, _ := req.GetArguments()["filename"].(string)
		name, ok := safeName(filename)
		if !ok {
			return mcp.NewToolResultError("invalid filename"), nil
		}
		if err := os.Remove(filepath.Join(filesDir, name)); err != nil {
			return mcp.NewToolResultError("file not found"), nil
		}
		return mcp.NewToolResultText("file deleted"), nil
	})

	// ---------- OUT-OF-BAND UPLOAD TOOL ----------

	getUploadURL := mcp.NewTool("get_upload_url",
		mcp.WithDescription("Get a temporary signed URL to upload a binary file (no base64). Valid for 5 minutes. After upload the file appears in list_files."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("Desired filename, e.g. photo.jpg")),
	)
	s.AddTool(getUploadURL, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename, _ := req.GetArguments()["filename"].(string)
		name, ok := safeName(filename)
		if !ok {
			return mcp.NewToolResultError("invalid filename"), nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !allowedExt[ext] {
			return mcp.NewToolResultError("file type not allowed: " + ext), nil
		}

		uploadID := uuid.New().String()
		token := generateToken(24)

		pending.Lock()
		pending.m[uploadID] = &pendingUpload{
			Filename:  name,
			ExpiresAt: time.Now().Add(5 * time.Minute),
			Token:     token,
		}
		pending.Unlock()

		uploadURL := fmt.Sprintf("%s?id=%s&token=%s", publicUploadBase, uploadID, token)

		return jsonResult(map[string]any{
			"upload_id":   uploadID,
			"upload_url":  uploadURL,
			"expires_in":  300,
			"method":      "POST",
			"form_field":  "file",
			"max_size_mb": 5,
			"curl_example": fmt.Sprintf(
				`curl -X POST -F "file=@%s" "%s"`,
				name, uploadURL,
			),
		})
	})

	log.Println("🚀 MCP server starting on stdio...")
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// ---------- HTTP upload server ----------

func startUploadServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		uploadID := r.URL.Query().Get("id")
		token := r.URL.Query().Get("token")
		if uploadID == "" || token == "" {
			http.Error(w, "missing id or token", http.StatusBadRequest)
			return
		}

		pending.Lock()
		p, exists := pending.m[uploadID]
		if !exists || time.Now().After(p.ExpiresAt) ||
			subtle.ConstantTimeCompare([]byte(token), []byte(p.Token)) != 1 {
			pending.Unlock()
			http.Error(w, "invalid or expired upload link", http.StatusUnauthorized)
			return
		}
		filename := p.Filename
		delete(pending.m, uploadID) // one-time use
		pending.Unlock()

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

		// Дополнительная проверка расширения (на всякий случай)
		if !allowedExt[strings.ToLower(filepath.Ext(filename))] {
			http.Error(w, "file type not allowed", http.StatusUnsupportedMediaType)
			return
		}

		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		dstPath := filepath.Join(filesDir, filename)
		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
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
		json.NewEncoder(w).Encode(map[string]any{
			"upload_id": uploadID,
			"filename":  filename,
			"size":      n,
			"status":    "uploaded",
		})
	})

	log.Printf("upload endpoint listening on %s", uploadAddr)
	if err := http.ListenAndServe(uploadAddr, mux); err != nil {
		log.Printf("upload server error: %v", err)
	}
}

// ---------- helpers ----------

func safeName(filename string) (string, bool) {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}