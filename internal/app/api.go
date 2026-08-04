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
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

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
	mux.HandleFunc("GET /api/discovered", s.discovered)
	mux.HandleFunc("POST /api/invites", s.createInvite)
	mux.HandleFunc("POST /api/join", s.join)
	mux.HandleFunc("POST /api/settings", s.settings)
	mux.HandleFunc("POST /api/permissions", s.permissions)
	mux.HandleFunc("POST /api/tunnels", s.addTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.removeTunnel)
	mux.HandleFunc("GET /api/tunnels", s.listTunnels)
	content, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(content)))
	s.server = &http.Server{Addr: addr, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second}
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) status(w http.ResponseWriter, _ *http.Request) {
	cfg := s.app.store.Snapshot()
	respond(w, map[string]any{"protocol": ProtocolVersion, "node_id": s.app.identity.ID(), "public_key": s.app.identity.PublicKeyString(), "config": cfg, "socks": s.app.socks.Address(), "endpoints": s.app.node.advertisedEndpoints()})
}
func (s *APIServer) peers(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.store.Snapshot().Peers)
}
func (s *APIServer) discovered(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.node.Discovered())
}
func (s *APIServer) listTunnels(w http.ResponseWriter, _ *http.Request) {
	respond(w, s.app.tunnels.Status())
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
	var req struct {
		NodeName     string `json:"node_name"`
		OfferExit    bool   `json:"offer_exit"`
		SOCKSEnabled bool   `json:"socks_enabled"`
		SelectedExit string `json:"selected_exit"`
	}
	if !decode(w, r, &req) {
		return
	}
	old := s.app.store.Snapshot()
	if err := s.app.store.Update(func(cfg *Config) error {
		if strings.TrimSpace(req.NodeName) != "" {
			cfg.NodeName = strings.TrimSpace(req.NodeName)
		}
		cfg.OfferExit = req.OfferExit
		cfg.SelectedExit = req.SelectedExit
		cfg.SOCKSEnabled = req.SOCKSEnabled
		return nil
	}); err != nil {
		problem(w, 500, err)
		return
	}
	if old.SOCKSEnabled != req.SOCKSEnabled {
		if req.SOCKSEnabled {
			if err := s.app.socks.Start(context.Background()); err != nil {
				problem(w, 500, err)
				return
			}
		} else {
			s.app.socks.Stop()
		}
	}
	respond(w, map[string]bool{"ok": true})
}

func (s *APIServer) permissions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID      string      `json:"peer_id"`
		Permissions Permissions `json:"permissions"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.app.store.Update(func(cfg *Config) error {
		peer, ok := cfg.Peers[req.PeerID]
		if !ok {
			return errors.New("unknown peer")
		}
		peer.Permissions = req.Permissions
		cfg.Peers[req.PeerID] = peer
		return nil
	})
	if err != nil {
		problem(w, 404, err)
		return
	}
	respond(w, map[string]bool{"ok": true})
}

func (s *APIServer) addTunnel(w http.ResponseWriter, r *http.Request) {
	var req TunnelConfig
	if !decode(w, r, &req) {
		return
	}
	if req.ID == "" {
		req.ID = randomID()
	}
	req.Enabled = true
	if err := s.app.tunnels.Add(context.Background(), req); err != nil {
		problem(w, 400, err)
		return
	}
	if err := s.app.store.Update(func(cfg *Config) error {
		cfg.Tunnels = append(cfg.Tunnels, req)
		return nil
	}); err != nil {
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

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		problem(w, 400, errors.New("invalid JSON"))
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	respond(w, map[string]string{"error": err.Error()})
}
func randomID() string {
	raw := make([]byte, 9)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}
