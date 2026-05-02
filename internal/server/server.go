package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/serjikisa/forge/internal/agent"
	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
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
	s.mux.HandleFunc("POST /v1/chat", s.handleChat)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	return s
}

func (s *Server) ListenAndServe(addr string) error {
	slog.Info("forge server listening", "addr", addr)
	return http.ListenAndServe(addr, s.mux)
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

	slog.Info("chat request", "message", req.Message)

	model := s.model
	if req.Model != "" {
		if sw, ok := s.provider.(provider.ModelSwitcher); ok {
			sw.SetModel(req.Model)
			model = req.Model
			defer sw.SetModel(s.model) // restore default after request
		}
	}
	slog.Info("using model", "model", model)

	headless := tui.NewHeadless()
	tools := tool.Registry()
	a := agent.New(s.provider, tools, headless, model)
	a.SetAutoApprove(true)
	a.Ask(context.Background(), req.Message)

	events := headless.Events()
	for _, e := range events {
		switch e.Type {
		case "text":
			slog.Info("chat response", "type", e.Type, "text", e.Text)
		case "tool_start", "tool_done":
			slog.Info("chat response", "type", e.Type, "tool", e.Tool, "detail", e.Detail)
		case "tool_error", "error":
			slog.Warn("chat response", "type", e.Type, "error", e.Error)
		default:
			slog.Info("chat response", "type", e.Type)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{Events: events})
}
