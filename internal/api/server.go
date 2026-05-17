package api

import (
	"encoding/json"
	"net/http"

	"github.com/Pingancoin/pacpool/internal/service"
)

type Server struct {
	service *service.Service
	mux     *http.ServeMux
}

func New(svc *service.Service) *Server {
	s := &Server{
		service: svc,
		mux:     http.NewServeMux(),
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/status", s.handleStatus)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "pacpool",
		"routes": []string{
			"/healthz",
			"/status",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.service.Snapshot()
	code := http.StatusOK
	if !status.Healthy {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, map[string]any{
		"healthy":    status.Healthy,
		"updated_at": status.UpdatedAt,
		"errors":     status.Errors,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.service.Snapshot())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
