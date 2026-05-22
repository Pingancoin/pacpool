package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Pingancoin/pacpool/internal/service"
)

type Options struct {
	AdminToken string
}

type Server struct {
	service    *service.Service
	mux        *http.ServeMux
	adminToken string
}

func New(svc *service.Service, opts ...Options) *Server {
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}
	s := &Server{
		service:    svc,
		mux:        http.NewServeMux(),
		adminToken: strings.TrimSpace(options.AdminToken),
	}
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/miner/", s.handleMiner)
	s.mux.HandleFunc("/admin", s.handleAdmin)
	s.mux.HandleFunc("/admin/login", s.handleAdminLogin)
	s.mux.HandleFunc("/admin/settings", s.handleAdminSettings)
	s.mux.HandleFunc("/payouts", s.handlePayouts)
	s.mux.HandleFunc("/payouts/execute", s.handlePayoutExecute)
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
	if err := renderDashboard(w, r, s.service); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "dashboard render failed"})
	}
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

func (s *Server) handleMiner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	address := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/miner/"))
	if address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "miner address required"})
		return
	}
	stats, found := s.service.MinerStats(address)
	writeJSON(w, http.StatusOK, map[string]any{
		"found": found,
		"miner": stats,
	})
}

func (s *Server) handlePayouts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	status := s.service.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"pending_payouts": status.Pool.PendingPayouts,
		"balances":        status.Pool.Balances,
		"payments":        status.Pool.Payments,
		"recent_rounds":   status.Pool.RecentRounds,
	})
}

func (s *Server) handlePayoutExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.authorizedAdmin(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin token required"})
		return
	}
	var req struct {
		TxID string `json:"txid"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payout request"})
		return
	}
	if strings.TrimSpace(req.TxID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "txid required"})
		return
	}
	record, ok := s.service.ExecutePayouts(req.TxID, req.Note)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no pending payouts"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"executed": true,
		"payment":  record,
		"status":   s.service.Snapshot().Pool,
	})
}

func (s *Server) authorizedAdmin(r *http.Request) bool {
	if s.adminToken == "" {
		return true
	}
	if cookie, err := r.Cookie("pacpool_admin"); err == nil && strings.TrimSpace(cookie.Value) == s.adminToken {
		return true
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token == s.adminToken
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if token := strings.TrimSpace(r.FormValue("token")); token != "" {
			return token == s.adminToken
		}
	}
	if token := strings.TrimSpace(r.Header.Get("X-PACPOOL-Admin-Token")); token != "" {
		return token == s.adminToken
	}
	const bearerPrefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(auth, bearerPrefix)) == s.adminToken
	}
	return false
}

func (s *Server) setAdminCookie(w http.ResponseWriter, r *http.Request) {
	if s.adminToken == "" {
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" && r.Method == http.MethodPost {
		token = strings.TrimSpace(r.FormValue("token"))
	}
	if token != s.adminToken {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "pacpool_admin",
		Value:    s.adminToken,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		MaxAge:   12 * 60 * 60,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
