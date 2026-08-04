package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

type testNode struct {
	id       *Identity
	store    *Store
	node     *Node
	cancel   context.CancelFunc
	endpoint string
}

func newTestNode(t *testing.T, name string) *testNode {
	t.Helper()
	dir := t.TempDir()
	id, err := LoadOrCreateIdentity(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	port := reservePort(t)
	endpoint := fmt.Sprintf("127.0.0.1:%d", port)
	if err := store.Update(func(cfg *Config) error {
		cfg.NodeName = name
		cfg.Listen = endpoint
		cfg.ManualEndpoints = []string{endpoint}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	node := NewNode(id, store, discardLogger(), NewEventLog(50))
	if err := node.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); node.Close() })
	return &testNode{id: id, store: store, node: node, cancel: cancel, endpoint: endpoint}
}

func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func trust(t *testing.T, owner *testNode, remote *testNode, granted, received Permissions, endpoints []string) {
	t.Helper()
	if endpoints == nil {
		endpoints = []string{remote.endpoint}
	}
	err := owner.store.Update(func(cfg *Config) error {
		cfg.Peers[remote.id.ID()] = Peer{ID: remote.id.ID(), Name: remote.store.Snapshot().NodeName, PublicKey: remote.id.PublicKeyString(), Endpoints: endpoints, Permissions: granted, Capabilities: received, AddedAt: time.Now()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEndToEndReverseRelayStream(t *testing.T) {
	origin := newTestNode(t, "origin")
	relay := newTestNode(t, "relay")
	destination := newTestNode(t, "destination")

	// Origin can use relay, while destination is deliberately unreachable
	// from both origin and relay and therefore registers an outbound backhaul.
	trust(t, origin, relay, Permissions{}, Permissions{Relay: true}, nil)
	trust(t, origin, destination, Permissions{}, Permissions{AccessLAN: true}, []string{"127.0.0.1:1"})
	trust(t, relay, origin, Permissions{Relay: true}, Permissions{}, nil)
	trust(t, relay, destination, Permissions{Relay: true}, Permissions{}, []string{"127.0.0.1:1"})
	trust(t, destination, origin, Permissions{AccessLAN: true}, Permissions{}, nil)
	trust(t, destination, relay, Permissions{}, Permissions{Relay: true}, nil)
	destination.node.ensureBackhaulClients(destination.node.ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		relay.node.backhaulMu.Lock()
		ready := len(relay.node.backhauls[destination.id.ID()]) > 0
		relay.node.backhaulMu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("destination did not register reverse backhaul")
		}
		time.Sleep(20 * time.Millisecond)
	}

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		c, err := echo.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()

	conn, route, _, err := origin.node.openStreamWithRoute(destination.id.ID(), "tunnel", echo.Addr().String())
	if err != nil {
		t.Fatalf("open through reverse relay: %v", err)
	}
	defer conn.Close()
	if route != "relay:"+relay.id.ID() {
		t.Fatalf("unexpected route %q", route)
	}
	message := []byte("lichen-reverse-relay-ok")
	if _, err := conn.Write(message); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(message))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(message) {
		t.Fatalf("echo mismatch: %q", got)
	}
}
