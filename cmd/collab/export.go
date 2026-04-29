package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

func exportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Dump the running session's conversation log as JSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(out)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Output path (default: session-<timestamp>.json)")
	return cmd
}

// exportFile is the export payload shape — a stable contract independent of
// the on-disk state file. Documented in docs/08-cli.md.
type exportFile struct {
	SessionID  string              `json:"session_id"`
	StartedAt  time.Time           `json:"started_at"`
	ExportedAt time.Time           `json:"exported_at"`
	Peers      []state.Peer        `json:"peers"`
	Entries    []protocol.LogEntry `json:"entries"`
}

func runExport(out string) error {
	f, err := state.Read()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "no active collab-ai session")
			os.Exit(1)
		}
		return err
	}

	if out == "" {
		out = fmt.Sprintf("session-%s.json", time.Now().UTC().Format("20060102-150405"))
	}

	payload := exportFile{
		SessionID:  f.SessionID,
		StartedAt:  f.StartedAt,
		ExportedAt: time.Now().UTC(),
		Peers:      f.Peers,
		Entries:    f.Entries,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	fmt.Fprintf(os.Stderr, "exported %d entries to %s\n", len(f.Entries), out)
	return nil
}
