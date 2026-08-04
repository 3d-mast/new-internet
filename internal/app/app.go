package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type App struct {
	dataDir  string
	logger   *log.Logger
	identity *Identity
	store    *Store
	node     *Node
	socks    *SOCKSServer
	tunnels  *TunnelManager
	api      *APIServer
	cancel   context.CancelFunc
}

func New(dataDir string, logger *log.Logger) (*App, error) {
	identity, err := LoadOrCreateIdentity(filepath.Join(dataDir, "identity.json"))
	if err != nil {
		return nil, err
	}
	store, err := LoadStore(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return nil, err
	}
	node := NewNode(identity, store, logger)
	instance := &App{dataDir: dataDir, logger: logger, identity: identity, store: store, node: node}
	instance.socks = NewSOCKSServer(node, store, logger)
	instance.tunnels = NewTunnelManager(node, logger)
	instance.api = NewAPIServer(instance, logger)
	return instance, nil
}

func (a *App) Start(parent context.Context, openBrowser bool) error {
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	if err := a.node.Start(ctx); err != nil {
		return fmt.Errorf("mesh listener: %w", err)
	}
	cfg := a.store.Snapshot()
	if cfg.SOCKSEnabled {
		if err := a.socks.Start(ctx); err != nil {
			a.logger.Printf("SOCKS disabled after startup error: %v", err)
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
		go func() {
			time.Sleep(350 * time.Millisecond)
			_ = openURL(a.UIURL())
		}()
	}
	return nil
}

func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	a.socks.Stop()
	a.node.Close()
}

func (a *App) NodeID() string { return a.identity.ID() }
func (a *App) UIURL() string  { return "http://" + a.store.Snapshot().APIListen }

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

func normalizeListenAddress(value string, fallbackPort int) string {
	if _, _, err := net.SplitHostPort(value); err == nil {
		return value
	}
	return fmt.Sprintf("127.0.0.1:%d", fallbackPort)
}
