package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcp_demo/store"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	filesDir    = "./uploads"
	maxFileSize = 5 << 20 // 5 MB
)

func main() {
	st := store.New()

	s := server.NewMCPServer(
		"users-tasks-server",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// ---------- USER TOOLS ----------

	// create_user
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

	// list_users
	listUsers := mcp.NewTool("list_users",
		mcp.WithDescription("List all users"),
	)
	s.AddTool(listUsers, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return jsonResult(st.ListUsers())
	})

	// get_user
	getUser := mcp.NewTool("get_user",
		mcp.WithDescription("Get a user by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("User ID")),
	)
	s.AddTool(getUser, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)
		u, err := st.GetUser(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(u)
	})

	// delete_user
	deleteUser := mcp.NewTool("delete_user",
		mcp.WithDescription("Delete a user and their tasks"),
		mcp.WithString("id", mcp.Required(), mcp.Description("User ID")),
	)
	s.AddTool(deleteUser, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)
		if err := st.DeleteUser(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("user deleted"), nil
	})

	// ---------- TASK TOOLS ----------

	// create_task
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

	// list_tasks
	listTasks := mcp.NewTool("list_tasks",
		mcp.WithDescription("List tasks, optionally filtered by user"),
		mcp.WithString("user_id", mcp.Description("Filter by user ID (optional)")),
	)
	s.AddTool(listTasks, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		userID, _ := args["user_id"].(string)
		return jsonResult(st.ListTasks(userID))
	})

	// update_task_status
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

	// delete_task
	deleteTask := mcp.NewTool("delete_task",
		mcp.WithDescription("Delete a task"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Task ID")),
	)
	s.AddTool(deleteTask, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)
		if err := st.DeleteTask(id); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("task deleted"), nil
	})

	// ---------- FILE TOOLS ----------

	// save_file
	saveFile := mcp.NewTool("save_file",
		mcp.WithDescription("Save a file on the server. Use encoding=base64 for binary files (max 5 MB)."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("File name, e.g. report.txt")),
		mcp.WithString("content", mcp.Required(), mcp.Description("File content")),
		mcp.WithString("encoding", mcp.Description("text (default) or base64")),
	)
	s.AddTool(saveFile, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		filename, _ := args["filename"].(string)
		content, _ := args["content"].(string)
		encoding, _ := args["encoding"].(string)

		name, ok := safeName(filename)
		if !ok {
			return mcp.NewToolResultError("invalid filename"), nil
		}
		if content == "" {
			return mcp.NewToolResultError("content is required"), nil
		}

		data := []byte(content)
		if encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(content)
			if err != nil {
				return mcp.NewToolResultError("invalid base64 content"), nil
			}
			data = decoded
		}
		if len(data) > maxFileSize {
			return mcp.NewToolResultError("file too large (max 5 MB)"), nil
		}

		if err := os.MkdirAll(filesDir, 0o755); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.WriteFile(filepath.Join(filesDir, name), data, 0o644); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(map[string]any{
			"filename": name,
			"size":     len(data),
			"saved_at": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// list_files
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

	// get_file (text files only)
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

	// delete_file
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

	log.Println("🚀 MCP server starting on stdio...")
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// safeName strips any directory part and rejects empty or hidden names,
// so a client can't write outside the uploads directory.
func safeName(filename string) (string, bool) {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}

// jsonResult marshals v to JSON and wraps it as a tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}