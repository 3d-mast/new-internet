package app

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
)

type runningTunnel struct {
	config      TunnelConfig
	ln          net.Listener
	err         string
	connections atomic.Int64
}

type TunnelManager struct {
	node   *Node
	logger *log.Logger
	events *EventLog
	mu     sync.RWMutex
	items  map[string]*runningTunnel
}

func NewTunnelManager(node *Node, logger *log.Logger, events *EventLog) *TunnelManager {
	return &TunnelManager{node: node, logger: logger, events: events, items: make(map[string]*runningTunnel)}
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
	item.err = ""
	m.mu.Unlock()
	go func() { <-ctx.Done(); m.Remove(cfg.ID) }()
	go m.acceptLoop(item)
	m.events.Add("success", "tunnel", "TCP-туннель запущен: "+cfg.Listen+" → "+cfg.Target, cfg.PeerID)
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
		item.connections.Add(1)
		go func() {
			defer conn.Close()
			upstream, err := m.node.OpenStream(item.config.PeerID, "tunnel", item.config.Target)
			if err != nil {
				m.logger.Printf("tunnel %s upstream: %v", item.config.ID, err)
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
		out = append(out, TunnelStatus{TunnelConfig: item.config, Active: item.ln != nil, Connections: item.connections.Load(), Error: item.err})
	}
	return out
}
