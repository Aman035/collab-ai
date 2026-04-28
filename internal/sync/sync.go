// Package sync keeps peers in sync. It subscribes to local Store changes,
// broadcasts them as WireMessages, and applies inbound messages to the local
// Store. Echo suppression compares envelope PeerID to our own — never
// Axl's lossy X-From-Peer-Id header.
//
// M1: log entries only. M2 adds the file index + fsnotify watcher.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Aman035/colabAI/internal/store"
	"github.com/Aman035/colabAI/internal/transport"
	"github.com/Aman035/colabAI/pkg/protocol"
)

// Engine connects the local Store to the Transport.
type Engine struct {
	store     *store.Store
	transport transport.Transport
	peerID    string
}

// New constructs an Engine. peerID must match transport.PeerID(); we accept
// it explicitly so the engine can run echo suppression without a
// Transport call on every message.
func New(s *store.Store, t transport.Transport, peerID string) *Engine {
	return &Engine{store: s, transport: t, peerID: peerID}
}

// Run blocks until ctx is cancelled, processing both directions:
//
//   - outbound: store changes (Source=local) are serialized and sent
//   - inbound:  transport messages are deserialized and applied to the store
func (e *Engine) Run(ctx context.Context) error {
	changes := e.store.Subscribe()
	inbound := e.transport.Receive()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ch, ok := <-changes:
			if !ok {
				return nil
			}
			if err := e.handleLocalChange(ch); err != nil {
				slog.Warn("sync: outbound failed", "err", err)
			}

		case msg, ok := <-inbound:
			if !ok {
				return nil
			}
			if err := e.handleInboundMessage(msg); err != nil {
				slog.Warn("sync: inbound failed", "err", err, "kind", msg.Kind, "peer", msg.PeerID)
			}
		}
	}
}

// handleLocalChange broadcasts a locally-originated change. Peer-origin
// changes are dropped here to prevent echo loops.
func (e *Engine) handleLocalChange(ch store.Change) error {
	if ch.Source != store.SourceLocal {
		return nil
	}
	switch ch.Kind {
	case store.KindEntryAppended:
		if ch.Entry == nil {
			return fmt.Errorf("entry_appended change with nil Entry")
		}
		raw, err := json.Marshal(*ch.Entry)
		if err != nil {
			return fmt.Errorf("marshal entry: %w", err)
		}
		return e.transport.Send(protocol.WireMessage{
			Kind:      protocol.KindLogEntry,
			Payload:   raw,
			PeerID:    e.peerID,
			Timestamp: ch.Entry.Timestamp,
		})
	default:
		// M2 adds file_upserted / file_deleted. Ignore unknowns for now.
		return nil
	}
}

// handleInboundMessage applies a peer-originated message to the local Store.
// Echo suppression: drop messages whose envelope PeerID matches our own.
func (e *Engine) handleInboundMessage(msg protocol.WireMessage) error {
	if msg.PeerID == e.peerID {
		return nil // echo
	}
	switch msg.Kind {
	case protocol.KindLogEntry:
		var entry protocol.LogEntry
		if err := json.Unmarshal(msg.Payload, &entry); err != nil {
			return fmt.Errorf("unmarshal log_entry: %w", err)
		}
		return e.store.AppendEntry(entry, store.SourcePeer)

	case protocol.KindHello, protocol.KindHelloAck, protocol.KindGoodbye:
		// Control messages are owned by the transport. If one slips
		// through, it's harmless; just log at debug.
		slog.Debug("sync: control message reached engine", "kind", msg.Kind)
		return nil

	case protocol.KindFileChunk:
		// M2 territory.
		return nil

	default:
		slog.Warn("sync: unknown message kind", "kind", msg.Kind)
		return nil
	}
}
