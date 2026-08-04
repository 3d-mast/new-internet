package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type App struct {
	dataDir      string
	identityPath string
	logger       *log.Logger
	identity     *Identity
	store        *Store
	events       *EventLog
	node         *Node
	socks        *SOCKSServer
	httpProxy    *HTTPProxy
	tunnels      *TunnelManager
	api          *APIServer
	ctx          context.Context
	cancel       context.CancelFunc
}

func New(dataDir string, logger *log.Logger) (*App, error) {
	identityPath := filepath.Join(dataDir, "identity.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		return nil, err
	}
	store, err := LoadStore(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return nil, err
	}
	events := NewEventLog(300)
	node := NewNode(identity, store, logger, events)
	instance := &App{dataDir: dataDir, identityPath: identityPath, logger: logger, identity: identity, store: store, events: events, node: node}
	instance.socks = NewSOCKSServer(node, store, logger, events)
	instance.httpProxy = NewHTTPProxy(node, store, logger)
	instance.tunnels = NewTunnelManager(node, logger, events)
	instance.api = NewAPIServer(instance, logger)
	return instance, nil
}

func (a *App) Start(parent context.Context, openBrowser bool) error {
	ctx, cancel := context.WithCancel(parent)
	a.ctx, a.cancel = ctx, cancel
	if err := a.node.Start(ctx); err != nil {
		return fmt.Errorf("mesh listener: %w", err)
	}
	cfg := a.store.Snapshot()
	if cfg.ProxyEnabled {
		if err := a.startProxies(); err != nil {
			a.events.Add("error", "proxy", "Прокси не запущен: "+err.Error(), "")
		}
		if cfg.SystemProxy {
			if err := setSystemProxy(true, cfg.HTTPListen); err != nil {
				a.events.Add("error", "system-proxy", err.Error(), "")
			}
		}
	}
	for _, tunnel := range cfg.Tunnels {
		if err := a.tunnels.Add(ctx, tunnel); err != nil {
			a.logger.Printf("tunnel %s: %v", tunnel.ID, err)
		}
	}
	if err := a.api.Start(ctx, cfg.APIListen); err != nil {
		return fmt.Errorf("API: %w", err)
	}
	if openBrowser {
		go func() { time.Sleep(350 * time.Millisecond); _ = openURL(a.UIURL()) }()
	}
	a.events.Add("success", "startup", "Lichen готов к работе", "")
	return nil
}

func (a *App) startProxies() error {
	if a.ctx == nil {
		return errors.New("application is not running")
	}
	if err := a.socks.Start(a.ctx); err != nil {
		return fmt.Errorf("SOCKS5: %w", err)
	}
	if err := a.httpProxy.Start(a.ctx); err != nil {
		a.socks.Stop()
		return fmt.Errorf("HTTP proxy: %w", err)
	}
	return nil
}

func (a *App) stopProxies() {
	_ = setSystemProxy(false, a.store.Snapshot().HTTPListen)
	a.socks.Stop()
	a.httpProxy.Stop()
}

func (a *App) ApplySettings(req SettingsRequest) error {
	old := a.store.Snapshot()
	name := strings.TrimSpace(req.NodeName)
	manual := make([]string, 0, len(req.ManualEndpoints))
	for _, endpoint := range req.ManualEndpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return fmt.Errorf("invalid public endpoint %q", endpoint)
		}
		manual = append(manual, endpoint)
	}
	if req.ProxyEnabled {
		peer, ok := old.Peers[req.SelectedExit]
		if !ok || !peer.Capabilities.UseExit {
			return errors.New("selected node did not grant internet exit capability")
		}
	}
	if req.SystemProxy && runtime.GOOS != "windows" {
		return errors.New("system proxy mode is currently supported on Windows only")
	}
	if err := a.store.Update(func(cfg *Config) error {
		if name != "" {
			cfg.NodeName = name
		}
		cfg.OfferExit = req.OfferExit
		cfg.ProxyEnabled = req.ProxyEnabled
		cfg.SystemProxy = req.SystemProxy && req.ProxyEnabled
		cfg.SelectedExit = req.SelectedExit
		cfg.AutoRelay = req.AutoRelay
		cfg.ManualEndpoints = manual
		return nil
	}); err != nil {
		return err
	}
	if old.ProxyEnabled != req.ProxyEnabled {
		if req.ProxyEnabled {
			if err := a.startProxies(); err != nil {
				_ = a.store.Replace(old)
				return err
			}
		} else {
			a.stopProxies()
		}
	}
	if req.ProxyEnabled {
		if req.SystemProxy != old.SystemProxy || req.SelectedExit != old.SelectedExit {
			if err := setSystemProxy(req.SystemProxy, a.store.Snapshot().HTTPListen); err != nil {
				return err
			}
		}
	} else if old.SystemProxy {
		_ = setSystemProxy(false, old.HTTPListen)
	}
	a.events.Add("success", "settings", "Настройки сети применены", "")
	return nil
}

func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.store.Snapshot().SystemProxy {
		_ = setSystemProxy(false, a.store.Snapshot().HTTPListen)
	}
	a.socks.Stop()
	a.httpProxy.Stop()
	a.node.Close()
}

func (a *App) NodeID() string { return a.identity.ID() }
func (a *App) UIURL() string  { return "http://" + a.store.Snapshot().APIListen }

func (a *App) Diagnostics() []DiagnosticItem {
	cfg := a.store.Snapshot()
	items := []DiagnosticItem{
		{Name: "Идентичность Ed25519", OK: a.identity.ID() != "", Details: a.identity.ID()},
		{Name: "Сетевой слушатель", OK: a.node.listener != nil, Details: cfg.Listen},
		{Name: "Локальная панель", OK: a.api.server != nil, Details: cfg.APIListen},
		{Name: "Каталог данных", OK: writableDirectory(a.dataDir), Details: a.dataDir},
	}
	if cfg.ProxyEnabled {
		items = append(items,
			DiagnosticItem{Name: "SOCKS5", OK: a.socks.Running(), Details: a.socks.Address()},
			DiagnosticItem{Name: "HTTP CONNECT", OK: a.httpProxy.Running(), Details: a.httpProxy.Address()},
		)
	}
	health := a.node.Health()
	for id, peer := range cfg.Peers {
		h := health[id]
		detail := peer.Name
		if h.Error != "" {
			detail += ": " + h.Error
		} else if h.Online {
			detail += fmt.Sprintf(": %d мс, %s", h.LatencyMS, h.Route)
		}
		items = append(items, DiagnosticItem{Name: "Узел " + shortID(id), OK: h.Online, Details: detail})
	}
	return items
}

func writableDirectory(path string) bool {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return false
	}
	file := filepath.Join(path, ".write-test")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(file)
	return true
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
