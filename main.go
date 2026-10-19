package main

import (
	"context"
	"encoding/json"
	"log"

	"mcp_demo/store"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

	log.Println("🚀 MCP server starting on stdio...")
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// jsonResult marshals v to JSON and wraps it as a tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}