package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() {
		t.Fatalf("identity changed: %s != %s", first.ID(), second.ID())
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("identity file missing: %v", err)
	}
}

func TestInviteRoundTrip(t *testing.T) {
	identity, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	node := NewNode(identity, store, discardLogger(), NewEventLog(50))
	token, err := node.CreateInvite(Permissions{UseExit: true, Relay: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeInvite(token)
	if err != nil {
		t.Fatal(err)
	}
	if payload.NodeID != identity.ID() || !payload.Permissions.UseExit || !payload.Permissions.Relay {
		t.Fatalf("unexpected invite: %+v", payload)
	}
}
