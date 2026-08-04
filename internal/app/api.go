package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

type SettingsRequest struct {
	NodeName        string   `json:"node_name"`
	OfferExit       bool     `json:"offer_exit"`
	ProxyEnabled    bool     `json:"proxy_enabled"`
	SystemProxy     bool     `json:"system_proxy"`
	SelectedExit    string   `json:"selected_exit"`
	AutoRelay       bool     `json:"auto_relay"`
	ManualEndpoints []string `json:"manual_endpoints"`
}

type APIServer struct {
	app    *App
	logger *log.Logger
	server *http.Server
}

func NewAPIServer(app *App, logger *log.Logger) *APIServer {
	return &APIServer{app: app, logger: logger}
}

func (s *APIServer) Start(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/peers", s.peers)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/discovered", s.discovered)
	mux.HandleFunc("GET /api/invites", s.invites)
	mux.HandleFunc("POST /api/invites", s.createInvite)
	mux.HandleFunc("DELETE /api/invites/{code}", s.revokeInvite)
	mux.HandleFunc("POST /api/join", s.join)
	mux.HandleFunc("POST /api/settings", s.settings)
	mux.HandleFunc("PATCH /api/peers/{id}", s.updatePeer)
	mux.HandleFunc("DELETE /api/peers/{id}", s.removePeer)
	mux.HandleFunc("POST /api/peers/{id}/probe", s.probePeer)
	mux.HandleFunc("POST /api/tunnels", s.addTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.removeTunnel)
	mux.HandleFunc("GET /api/tunnels", s.listTunnels)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/diagnostics", s.diagnostics)
	mux.HandleFunc("POST /api/profile/export", s.exportProfile)
	mux.HandleFunc("POST /api/profile/import", s.importProfile)
	content, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(content)))
	s.server = &http.Server{Addr: addr, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdown)
	}()
	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Printf("API server: %v", err)
		}
	}()
	return nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		if r.URL.Path != "/" && strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				problem(w, http.StatusForbidden, errors.New("cross-site request blocked"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				problem(w, http.StatusForbidden, errors.New("origin rejected"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sameOrigin(origin, host string) bool {
	return origin == "http://"+host || origin == "https://"+host
}

func (s *APIServer) status(w http.ResponseWriter, _ *http.Request) {
	cfg := s.app.store.Snapshot()
	respond(w, map[string]any{
		"product": ProductName, "protocol": ProtocolVersion, "node_id": s.app.identity.ID(), "public_key": s.app.identity.PublicKeyString(),
		"config": cfg, "socks": s.app.socks.Address(), "http_proxy": s.app.httpProxy.Address(), "endpoints": s.app.node.advertisedEndpoints(),
	})
}
func (s *APIServer) peers(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.store.Snapshot().Peers)
}
func (s *APIServer) health(w http.ResponseWriter, _ *http.Request) { respond(w, s.app.node.Health()) }
func (s *APIServer) discovered(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.node.Discovered())
}
func (s *APIServer) listTunnels(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.tunnels.Status())
}
func (s *APIServer) events(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.events.List(100))
}
func (s *APIServer) diagnostics(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.Diagnostics())
}

func (s *APIServer) invites(w http.ResponseWriter, _ *http.Request) {
	items := make([]Invitation, 0)
	for _, invite := range s.app.store.Snapshot().Invitations {
		items = append(items, invite)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	respond(w, items)
}

func (s *APIServer) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Permissions Permissions `json:"permissions"`
		TTLMinutes  int         `json:"ttl_minutes"`
	}
	if !decode(w, r, &req) {
		return
	}
	token, err := s.app.node.CreateInvite(req.Permissions, time.Duration(req.TTLMinutes)*time.Minute)
	if err != nil {
		problem(w, http.StatusBadRequest, err)
		return
	}
	respond(w, map[string]string{"token": token})
}
func (s *APIServer) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.app.node.RevokeInvite(r.PathValue("code")); err != nil {
		problem(w, 404, err)
		return
	}
	respond(w, map[string]bool{"ok": true})
}
func (s *APIServer) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.app.node.JoinInvite(strings.TrimSpace(req.Token)); err != nil {
		problem(w, http.StatusBadRequest, err)
		return
	}
	respond(w, map[string]bool{"ok": true})
}
func (s *APIServer) settings(w http.ResponseWriter, r *http.Request) {
	var req SettingsRequest
	if !decode(w, r, &req) {
		return
	}
	if err := s.app.ApplySettings(req); err != nil {
		problem(w, 400, err)
		return
	}
	respond(w, map[string]bool{"ok": true})
}

func (s *APIServer) updatePeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string      `json:"name"`
		Endpoints   []string    `json:"endpoints"`
		Permissions Permissions `json:"permissions"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.app.node.UpdatePeer(r.PathValue("id"), req.Name, req.Endpoints, req.Permissions); err != nil {
		problem(w, 404, err)
		return
	}
	s.app.events.Add("info", "permissions", "Права узла обновлены", r.PathValue("id"))
	respond(w, map[string]bool{"ok": true})
}
func (s *APIServer) removePeer(w http.ResponseWriter, r *http.Request) {
	if err := s.app.node.RemovePeer(r.PathValue("id")); err != nil {
		problem(w, 404, err)
		return
	}
	respond(w, map[string]bool{"ok": true})
}
func (s *APIServer) probePeer(w http.ResponseWriter, r *http.Request) {
	respond(w, s.app.node.Probe(r.PathValue("id")))
}

func (s *APIServer) addTunnel(w http.ResponseWriter, r *http.Request) {
	var req TunnelConfig
	if !decode(w, r, &req) {
		return
	}
	if req.ID == "" {
		req.ID = randomID()
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.Listen + " → " + req.Target
	}
	req.Enabled = true
	if err := s.app.tunnels.Add(s.app.ctx, req); err != nil {
		problem(w, 400, err)
		return
	}
	if err := s.app.store.Update(func(cfg *Config) error { cfg.Tunnels = append(cfg.Tunnels, req); return nil }); err != nil {
		s.app.tunnels.Remove(req.ID)
		problem(w, 500, err)
		return
	}
	respond(w, req)
}
func (s *APIServer) removeTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.app.tunnels.Remove(id)
	_ = s.app.store.Update(func(cfg *Config) error {
		out := cfg.Tunnels[:0]
		for _, item := range cfg.Tunnels {
			if item.ID != id {
				out = append(out, item)
			}
		}
		cfg.Tunnels = out
		return nil
	})
	respond(w, map[string]bool{"ok": true})
}

func (s *APIServer) exportProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	token, err := s.app.ExportProfile(req.Password)
	if err != nil {
		problem(w, 400, err)
		return
	}
	respond(w, map[string]string{"profile": token})
}
func (s *APIServer) importProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Profile  string `json:"profile"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.app.ImportProfile(strings.TrimSpace(req.Profile), req.Password); err != nil {
		problem(w, 400, err)
		return
	}
	respond(w, map[string]any{"ok": true, "restart_required": true})
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		problem(w, 415, errors.New("application/json required"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		problem(w, 400, errors.New("invalid JSON: "+err.Error()))
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func randomID() string {
	raw := make([]byte, 9)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}
