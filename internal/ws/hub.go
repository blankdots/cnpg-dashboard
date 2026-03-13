package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/blankdots/cnpg-dashboard/internal/store"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
)

// InboundMsg is a command sent from the browser to the backend.
type InboundMsg struct {
	Action  string          `json:"action"`
	Payload json.RawMessage  `json:"payload,omitempty"`
}

// OutboundMsg wraps every message sent to the browser.
type OutboundMsg struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// CommandHandler is a function that handles a specific action.
type CommandHandler func(ctx context.Context, payload json.RawMessage) (interface{}, error)

// Hub manages all active WebSocket connections and broadcasts store events.
type Hub struct {
	store    *store.Store
	handlers map[string]CommandHandler

	mu      sync.RWMutex
	clients map[*client]struct{}
}

// New creates a Hub. Call Serve() to handle upgrade requests.
func New(s *store.Store) *Hub {
	return &Hub{
		store:    s,
		handlers: make(map[string]CommandHandler),
		clients:  make(map[*client]struct{}),
	}
}

// Store returns the hub's store (for command handlers that need to trigger store updates).
func (h *Hub) Store() *store.Store {
	return h.store
}

// Register adds a CommandHandler for the given action name.
func (h *Hub) Register(action string, fn CommandHandler) {
	h.mu.Lock()
	h.handlers[action] = fn
	h.mu.Unlock()
}

// Serve upgrades the HTTP connection to WebSocket and starts read/write loops.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("ws upgrade failed", slog.Any("err", err))
		return
	}

	c := &client{
		conn: conn,
		send: make(chan OutboundMsg, 64),
	}
	h.addClient(c)
	defer h.removeClient(c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	eventCh := h.store.Subscribe()
	defer h.store.Unsubscribe(eventCh)

	h.sendSnapshot(ctx, c)

	// After a short delay, send full state again so clients get Backup-derived data (count/size) that may have arrived after initial snapshot
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			payload := map[string]interface{}{
				"clusters":    h.store.Clusters(),
				"objectstores": h.store.Barmans(),
			}
			select {
			case c.send <- OutboundMsg{Type: "full_state", Payload: payload}:
			default:
			}
		}
	}()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-c.send:
				if !ok {
					return
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
				if err := wsjson.Write(writeCtx, conn, msg); err != nil {
					writeCancel()
					cancel()
					return
				}
				writeCancel()
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, writeTimeout)
				if err := conn.Ping(pingCtx); err != nil {
					pingCancel()
					cancel()
					return
				}
				pingCancel()
			case event, ok := <-eventCh:
				if !ok {
					return
				}
				select {
				case c.send <- OutboundMsg{Type: "event", Payload: event}:
				default:
					slog.Warn("ws send buffer full, dropping event",
						slog.String("resource", event.ResourceKind))
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			var msg InboundMsg
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				return
			}
			h.handleCommand(ctx, c, msg)
		}
	}()

	wg.Wait()
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		slog.Debug("ws close", slog.Any("err", err))
	}
}

func (h *Hub) sendSnapshot(ctx context.Context, c *client) {
	for _, cluster := range h.store.Clusters() {
		c.send <- OutboundMsg{
			Type: "event",
			Payload: store.Event{
				Type:         store.EventAdded,
				ResourceKind: store.ResourceCluster,
				Resource:     cluster,
			},
		}
	}
	for _, b := range h.store.Barmans() {
		c.send <- OutboundMsg{
			Type: "event",
			Payload: store.Event{
				Type:         store.EventAdded,
				ResourceKind: store.ResourceBarman,
				Resource:     b,
			},
		}
	}
}

func (h *Hub) handleCommand(ctx context.Context, c *client, msg InboundMsg) {
	h.mu.RLock()
	fn, ok := h.handlers[msg.Action]
	h.mu.RUnlock()

	if !ok {
		c.send <- OutboundMsg{
			Type:    "error",
			Payload: map[string]string{"error": "unknown action: " + msg.Action},
		}
		slog.Warn("ws unknown action", slog.String("action", msg.Action))
		return
	}

	result, err := fn(ctx, msg.Payload)
	if err != nil {
		c.send <- OutboundMsg{
			Type:    "error",
			Payload: map[string]string{"action": msg.Action, "error": err.Error()},
		}
		slog.Error("ws command failed",
			slog.String("action", msg.Action),
			slog.Any("err", err),
		)
		return
	}

	c.send <- OutboundMsg{Type: "ack", Payload: map[string]interface{}{
		"action": msg.Action,
		"result": result,
	}}
}

func (h *Hub) addClient(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	slog.Debug("ws client connected", slog.Int("total", h.clientCount()))
}

func (h *Hub) removeClient(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	slog.Debug("ws client disconnected", slog.Int("total", h.clientCount()))
}

func (h *Hub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

type client struct {
	conn *websocket.Conn
	send chan OutboundMsg
}
