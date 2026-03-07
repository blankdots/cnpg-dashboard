package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/blankdots/cnpg-dashboard/internal/store"
	"github.com/blankdots/cnpg-dashboard/internal/ws"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	store *store.Store
	hub   *ws.Hub
}

// New creates an API handler, registers all routes on mux, and returns
// the WebSocket hub so the caller can register command handlers on it.
func New(s *store.Store, mux *http.ServeMux) (*Handler, *ws.Hub) {
	hub := ws.New(s)

	h := &Handler{store: s, hub: hub}

	// REST — useful for scripting, health checks, initial page load
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /api/clusters", h.listClusters)
	mux.HandleFunc("GET /api/objectstores", h.listObjectStores)

	// WebSocket — single connection for live events + commands
	mux.HandleFunc("GET /api/ws", hub.Serve)

	return h, hub
}

// ── REST ──────────────────────────────────────────────────────────────────

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) listClusters(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	clusters := h.store.Clusters()

	if ns != "" {
		filtered := clusters[:0]
		for _, c := range clusters {
			if strings.Contains(c.Namespace, ns) {
				filtered = append(filtered, c)
			}
		}
		clusters = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": clusters,
		"total": len(clusters),
	})
}

func (h *Handler) listObjectStores(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	barmans := h.store.Barmans()

	if ns != "" {
		filtered := barmans[:0]
		for _, b := range barmans {
			if strings.Contains(b.Namespace, ns) {
				filtered = append(filtered, b)
			}
		}
		barmans = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": barmans,
		"total": len(barmans),
	})
}

// ── Helpers ────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode error", slog.Any("err", err))
	}
}
