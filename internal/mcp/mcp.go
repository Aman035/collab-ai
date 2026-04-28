// Package mcp exposes the local Store to an AI coding agent over the MCP
// stdio transport. M1 ships two tools: get_shared_log and post_to_log.
// list_shared_files lands in M2.
package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oklog/ulid/v2"

	"github.com/Aman035/colabAI/internal/store"
	"github.com/Aman035/colabAI/pkg/protocol"
)

// Server is a thin wrapper around mcp-go's stdio server, configured with
// our two tools backed by a local Store.
type Server struct {
	srv    *server.MCPServer
	store  *store.Store
	peerID string
}

// New constructs a Server. peerID is stamped onto every entry written via
// post_to_log.
func New(s *store.Store, peerID string) *Server {
	srv := server.NewMCPServer("collab-ai", protocol.Version)
	out := &Server{srv: srv, store: s, peerID: peerID}

	srv.AddTool(
		mcp.NewTool("get_shared_log",
			mcp.WithDescription("Return the full collab-ai conversation log, optionally filtered to entries after a timestamp."),
			mcp.WithString("since_timestamp",
				mcp.Description("RFC3339 timestamp; if set, only return entries strictly after this time."),
			),
		),
		out.handleGetSharedLog,
	)

	srv.AddTool(
		mcp.NewTool("post_to_log",
			mcp.WithDescription("Append a new entry to the shared collab-ai conversation log. The entry is broadcast to all peers."),
			mcp.WithString("role",
				mcp.Description(`"user" or "assistant".`),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("The entry text."),
				mcp.Required(),
			),
		),
		out.handlePostToLog,
	)

	return out
}

// ServeStdio runs the MCP server on stdio. Blocks until ctx is cancelled or
// the agent disconnects.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.ServeStdio(s.srv)
}

func (s *Server) handleGetSharedLog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	since := time.Time{}
	if v, ok := args["since_timestamp"].(string); ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("since_timestamp not RFC3339: %v", err)), nil
		}
		since = t
	}

	entries := s.store.EntriesSince(since)
	if len(entries) > 500 {
		entries = entries[:500]
	}

	return mcp.NewToolResultJSON(map[string]any{
		"entries": entries,
	})
}

func (s *Server) handlePostToLog(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	role, _ := args["role"].(string)
	if role != protocol.RoleUser && role != protocol.RoleAssistant {
		return mcp.NewToolResultError(`role must be "user" or "assistant"`), nil
	}
	content, _ := args["content"].(string)
	if content == "" {
		return mcp.NewToolResultError("content is required"), nil
	}

	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), rand.Reader).String()
	entry := protocol.LogEntry{
		ID:        id,
		Timestamp: now,
		PeerID:    s.peerID,
		Role:      role,
		Content:   content,
	}
	if err := s.store.AppendEntry(entry, store.SourceLocal); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("append: %v", err)), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"entry_id":  id,
		"timestamp": now.Format(time.RFC3339),
	})
}
