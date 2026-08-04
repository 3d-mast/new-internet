package app

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
)

type runningTunnel struct {
	config TunnelConfig
	ln     net.Listener
	err    string
}

type TunnelManager struct {
	node   *Node
	logger *log.Logger
	mu     sync.RWMutex
	items  map[string]*runningTunnel
}

func NewTunnelManager(node *Node, logger *log.Logger) *TunnelManager {
	return &TunnelManager{node: node, logger: logger, items: make(map[string]*runningTunnel)}
}

func (m *TunnelManager) Add(ctx context.Context, cfg TunnelConfig) error {
	if cfg.ID == "" || cfg.PeerID == "" || cfg.Listen == "" || cfg.Target == "" {
		return errors.New("incomplete tunnel configuration")
	}
	m.mu.Lock()
	if _, exists := m.items[cfg.ID]; exists {
		m.mu.Unlock()
		return errors.New("tunnel already exists")
	}
	item := &runningTunnel{config: cfg}
	m.items[cfg.ID] = item
	m.mu.Unlock()
	if !cfg.Enabled {
		return nil
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		m.mu.Lock()
		item.err = err.Error()
		m.mu.Unlock()
		return err
	}
	m.mu.Lock()
	item.ln = ln
	m.mu.Unlock()
	go func() { <-ctx.Done(); m.Remove(cfg.ID) }()
	go m.acceptLoop(item)
	return nil
}

func (m *TunnelManager) acceptLoop(item *runningTunnel) {
	for {
		conn, err := item.ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				m.logger.Printf("tunnel %s: %v", item.config.ID, err)
			}
			return
		}
		go func() {
			defer conn.Close()
			upstream, err := m.node.OpenStream(item.config.PeerID, "tunnel", item.config.Target)
			if err != nil {
				return
			}
			defer upstream.Close()
			proxyDuplex(conn, upstream)
		}()
	}
}

func (m *TunnelManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, ok := m.items[id]; ok {
		if item.ln != nil {
			_ = item.ln.Close()
		}
		delete(m.items, id)
	}
}

func (m *TunnelManager) Status() []TunnelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TunnelStatus, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, TunnelStatus{TunnelConfig: item.config, Active: item.ln != nil, Error: item.err})
	}
	return out
}
