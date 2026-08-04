package app

import (
	"log"
	"path/filepath"
	"testing"
)

func TestEncryptedProfileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	instance, err := New(dir, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	originalID := instance.NodeID()
	if err := instance.store.Update(func(cfg *Config) error { cfg.NodeName = "backup-node"; return nil }); err != nil {
		t.Fatal(err)
	}
	token, err := instance.ExportProfile("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	other, err := New(otherDir, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.ImportProfile(token, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(otherDir, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NodeID() != originalID {
		t.Fatalf("identity was not restored")
	}
	if reloaded.store.Snapshot().NodeName != "backup-node" {
		t.Fatalf("config was not restored")
	}
	_ = filepath.Separator
}
