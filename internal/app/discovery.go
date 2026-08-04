package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const discoveryGroup = "239.42.42.42:47830"

type announcement struct {
	Version   string `json:"version"`
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Port      int    `json:"port"`
	Time      int64  `json:"time"`
	Signature string `json:"signature"`
}

func (a announcement) signingBytes() []byte {
	a.Signature = ""
	raw, _ := json.Marshal(a)
	return raw
}

type Discovery struct {
	identity *Identity
	store    *Store
	logger   *log.Logger
	mu       sync.RWMutex
	nodes    map[string]DiscoveredNode
}

func NewDiscovery(identity *Identity, store *Store, logger *log.Logger) *Discovery {
	return &Discovery{identity: identity, store: store, logger: logger, nodes: make(map[string]DiscoveredNode)}
}

func (d *Discovery) Start(ctx context.Context) {
	go d.listen(ctx)
	go d.broadcast(ctx)
	go d.cleanup(ctx)
}

func (d *Discovery) Nodes() []DiscoveredNode {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DiscoveredNode, 0, len(d.nodes))
	for _, node := range d.nodes {
		out = append(out, node)
	}
	return out
}

func (d *Discovery) listen(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp4", discoveryGroup)
	if err != nil {
		d.logger.Printf("discovery resolve: %v", err)
		return
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		d.logger.Printf("discovery listen unavailable: %v", err)
		return
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(64 * 1024)
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			d.logger.Printf("discovery read: %v", err)
			continue
		}
		var msg announcement
		if json.Unmarshal(buf[:n], &msg) != nil || msg.Version != ProtocolVersion || msg.NodeID == d.identity.ID() {
			continue
		}
		if msg.NodeID != nodeIDMust(msg.PublicKey) || !verifySignature(msg.PublicKey, msg.signingBytes(), msg.Signature) {
			continue
		}
		host := remote.IP.String()
		endpoint := net.JoinHostPort(host, strconv.Itoa(msg.Port))
		d.mu.Lock()
		d.nodes[msg.NodeID] = DiscoveredNode{ID: msg.NodeID, Name: msg.Name, PublicKey: msg.PublicKey, Endpoint: endpoint, SeenAt: time.Now()}
		d.mu.Unlock()
	}
}

func (d *Discovery) broadcast(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp4", discoveryGroup)
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		d.logger.Printf("discovery dial: %v", err)
		return
	}
	defer conn.Close()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	send := func() {
		cfg := d.store.Snapshot()
		_, portText, err := net.SplitHostPort(cfg.Listen)
		if err != nil {
			return
		}
		port, _ := strconv.Atoi(portText)
		msg := announcement{Version: ProtocolVersion, NodeID: d.identity.ID(), Name: cfg.NodeName, PublicKey: d.identity.PublicKeyString(), Port: port, Time: time.Now().Unix()}
		msg.Signature = d.identity.Sign(msg.signingBytes())
		raw, _ := json.Marshal(msg)
		_, _ = conn.Write(raw)
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func (d *Discovery) cleanup(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-15 * time.Second)
			d.mu.Lock()
			for id, node := range d.nodes {
				if node.SeenAt.Before(cutoff) {
					delete(d.nodes, id)
				}
			}
			d.mu.Unlock()
		}
	}
}

func nodeIDMust(encoded string) string {
	key, err := parsePublicKey(encoded)
	if err != nil {
		return ""
	}
	return nodeID(key)
}

func endpointPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return DefaultMeshPort
	}
	n, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil {
		return DefaultMeshPort
	}
	return n
}

func endpointForRemote(remote net.Addr, port int) string {
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		return fmt.Sprintf("%s:%d", remote.String(), port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
