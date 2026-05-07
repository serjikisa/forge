// Package server exposes forge as a REST API. It provides /v1/chat for sending
// messages, /health for liveness checks, and / for endpoint discovery.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/serjikisa/forge/internal/agent"
	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
	"github.com/serjikisa/forge/pkg/slogr"
)

type ChatRequest struct {
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
}

type ChatResponse struct {
	Events []tui.Event `json:"events"`
}

type Server struct {
	provider provider.Provider
	model    string
	mux      *http.ServeMux
}

func New(p provider.Provider, model string) *Server {
	s := &Server{provider: p, model: model, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.handleDiscovery)
	s.mux.HandleFunc("POST /v1/chat", s.handleChat)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	return s
}

func (s *Server) ListenAndServe(addr string) error {
	slogr.Info("forge server listening", "addr", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":  "forge",
		"model": s.model,
		"endpoints": []map[string]string{
			{"method": "GET", "path": "/", "description": "API discovery"},
			{"method": "GET", "path": "/health", "description": "Health check"},
			{"method": "POST", "path": "/v1/chat", "description": "Send a chat message. Body: {\"message\": \"...\", \"model\": \"...(optional)\"}"},
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}

	slogr.Info("chat request", "message", req.Message)

	model := s.model
	if req.Model != "" {
		if sw, ok := s.provider.(provider.ModelSwitcher); ok {
			sw.SetModel(req.Model)
			model = req.Model
			defer sw.SetModel(s.model) // restore default after request
		}
	}
	slogr.Info("using model", "model", model)

	headless := tui.NewHeadless()
	tools := tool.Registry()
	a := agent.New(s.provider, tools, headless, model)
	a.SetAutoApprove(true)
	a.Ask(context.Background(), req.Message)

	events := headless.Events()
	for _, e := range events {
		switch e.Type {
		case "text":
			slogr.Info("chat response", "type", e.Type, "text", e.Text)
		case "tool_start", "tool_done":
			slogr.Info("chat response", "type", e.Type, "tool", e.Tool, "detail", e.Detail)
		case "tool_error", "error":
			slogr.Warn("chat response", "type", e.Type, "error", e.Error)
		default:
			slogr.Info("chat response", "type", e.Type)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{Events: events})
}
