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

	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

// Server is a thin wrapper around mcp-go's stdio server, configured with
// our tools backed by a local Store + the active transport (for presence
// + cross-peer calls).
type Server struct {
	srv    *server.MCPServer
	store  *store.Store
	tx     transport.Transport
	peerID string
}

// New constructs a Server. peerID is stamped onto every entry written via
// post_to_log; the transport is used by tools that broadcast (set_status,
// ask_peer) and look up peers by handle.
func New(s *store.Store, t transport.Transport, peerID string) *Server {
	srv := server.NewMCPServer("collab-ai", protocol.Version)
	out := &Server{srv: srv, store: s, tx: t, peerID: peerID}

	srv.AddTool(
		mcp.NewTool("get_shared_log",
			mcp.WithDescription(
				"Read the shared collab-ai conversation log — the running thread of "+
					"messages between you and your pair partner's AI agent. Call this "+
					"at the start of any new task in this session to catch up on prior "+
					"context, and again whenever the user references something that "+
					"may have come from the other side. Returns all entries in "+
					"chronological order with each entry's author handle."),
			mcp.WithString("since_timestamp",
				mcp.Description("Optional RFC3339 timestamp. If set, returns only entries strictly after this time — useful when polling for new messages without re-reading old ones."),
			),
		),
		out.handleGetSharedLog,
	)

	srv.AddTool(
		mcp.NewTool("post_to_log",
			mcp.WithDescription(
				"Share a message with your pair partner's AI agent through the "+
					"collab-ai shared log. Call this proactively whenever you've "+
					"reached a finding, decision, plan, or insight the other side "+
					"would want to see — they call get_shared_log to read it. "+
					"Treat the log like a group chat: post when something changes "+
					"shared understanding. Skip filler and \"thinking out loud\"."),
			mcp.WithString("role",
				mcp.Description(`"assistant" when sharing your own reasoning or output. "user" when echoing the human's words verbatim.`),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("The message text. Markdown is fine; concise is better than long."),
				mcp.Required(),
			),
		),
		out.handlePostToLog,
	)

	srv.AddTool(
		mcp.NewTool("list_shared_files",
			mcp.WithDescription(
				"List files in the shared workspace (./shared/) — these sync "+
					"automatically between all peers in this collab-ai session. "+
					"Call this whenever you need to see what your pair partner has "+
					"put up for collaboration, or before reading/writing files in "+
					"the shared dir. Files outside ./shared/ are local-only and "+
					"are not visible to peers."),
			mcp.WithString("path",
				mcp.Description("Optional path prefix (relative to ./shared/). If empty, returns every shared file."),
			),
		),
		out.handleListSharedFiles,
	)

	srv.AddTool(
		mcp.NewTool("ask_peer",
			mcp.WithDescription(
				"Send a direct question to a specific peer's agent. Use this "+
					"when you need their input on something — a code review, a "+
					"design opinion, a status update — instead of just "+
					"broadcasting via post_to_log. The peer's agent sees the "+
					"question on their next get_shared_log call (kind=\"question\", "+
					"target_peer=their handle) and is expected to call "+
					"respond_to_peer with the answer."),
			mcp.WithString("target_peer",
				mcp.Description("The peer's friendly handle (e.g. \"clever-otter\"). Get the list from list_peers context — typically you already know who the peers are from the prior log."),
				mcp.Required(),
			),
			mcp.WithString("question",
				mcp.Description("The question text. Be specific — your peer's agent decides whether to answer based on this alone."),
				mcp.Required(),
			),
		),
		out.handleAskPeer,
	)

	srv.AddTool(
		mcp.NewTool("respond_to_peer",
			mcp.WithDescription(
				"Answer a question another peer's agent directed at you. Call "+
					"this when get_shared_log returned an entry with "+
					"kind=\"question\" and target_peer set to YOUR handle — "+
					"that's a direct request you're expected to address. "+
					"Use the question entry's id as question_id."),
			mcp.WithString("question_id",
				mcp.Description("The id of the question entry you're answering."),
				mcp.Required(),
			),
			mcp.WithString("answer",
				mcp.Description("Your answer text."),
				mcp.Required(),
			),
		),
		out.handleRespondToPeer,
	)

	srv.AddTool(
		mcp.NewTool("set_status",
			mcp.WithDescription(
				"Tell your pair partner's agent what you're currently working on. "+
					"A short, ongoing-tense phrase like \"reading cache.go\", "+
					"\"writing the test harness\", or \"stuck on the auth bug\". "+
					"Update it whenever you switch contexts so your partner can "+
					"see what's happening live in the peer roster. The status "+
					"replaces any previous one."),
			mcp.WithString("status",
				mcp.Description("Short ongoing-tense phrase. Empty string clears the status."),
				mcp.Required(),
			),
		),
		out.handleSetStatus,
	)

	return out
}

// ServeStdio runs the MCP server on stdio. Blocks until ctx is cancelled or
// the agent disconnects. Used when collab-ai is itself spawned as an MCP
// child by another process (Claude Code, Cursor, etc).
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.ServeStdio(s.srv)
}

// ServeHTTP runs the MCP server as a streamable-HTTP endpoint at addr/mcp.
// Used when collab-ai is the parent and launches the AI agent as a child;
// the agent's MCP client connects back over loopback HTTP. Blocks until ctx
// is cancelled.
func (s *Server) ServeHTTP(ctx context.Context, addr string) error {
	httpSrv := server.NewStreamableHTTPServer(s.srv,
		server.WithEndpointPath("/mcp"),
		server.WithStateLess(true),
	)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Start(addr) }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
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

func (s *Server) handleListSharedFiles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	prefix, _ := args["path"].(string)

	files := s.store.ListFiles(prefix)
	return mcp.NewToolResultJSON(map[string]any{
		"files": files,
	})
}

func (s *Server) handleSetStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	status, _ := args["status"].(string)
	if s.tx == nil {
		return mcp.NewToolResultError("transport not available"), nil
	}
	if err := s.tx.BroadcastStatus(status); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("broadcast: %v", err)), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"status": status,
	})
}

func (s *Server) handleAskPeer(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	target, _ := args["target_peer"].(string)
	question, _ := args["question"].(string)
	if target == "" || question == "" {
		return mcp.NewToolResultError("target_peer and question are required"), nil
	}
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), rand.Reader).String()
	entry := protocol.LogEntry{
		ID:         id,
		Timestamp:  now,
		PeerID:     s.peerID,
		Role:       protocol.RoleAssistant,
		Content:    question,
		Kind:       protocol.EntryKindQuestion,
		TargetPeer: target,
	}
	if err := s.store.AppendEntry(entry, store.SourceLocal); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("append: %v", err)), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"question_id": id,
		"target_peer": target,
		"timestamp":   now.Format(time.RFC3339),
	})
}

func (s *Server) handleRespondToPeer(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	questionID, _ := args["question_id"].(string)
	answer, _ := args["answer"].(string)
	if questionID == "" || answer == "" {
		return mcp.NewToolResultError("question_id and answer are required"), nil
	}
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), rand.Reader).String()
	entry := protocol.LogEntry{
		ID:         id,
		Timestamp:  now,
		PeerID:     s.peerID,
		Role:       protocol.RoleAssistant,
		Content:    answer,
		Kind:       protocol.EntryKindAnswer,
		QuestionID: questionID,
	}
	if err := s.store.AppendEntry(entry, store.SourceLocal); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("append: %v", err)), nil
	}
	return mcp.NewToolResultJSON(map[string]any{
		"answer_id":   id,
		"question_id": questionID,
		"timestamp":   now.Format(time.RFC3339),
	})
}
