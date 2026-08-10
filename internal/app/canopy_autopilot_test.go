package app

import (
	"testing"
	"time"
)

func TestRouteCandidateRequiresConfirmationWhileCurrentIsHealthy(t *testing.T) {
	a := &Autopilot{}
	cfg := Config{SelectedExit: "current"}
	health := map[string]PeerHealth{
		"current": {PeerID: "current", Online: true},
		"better":  {PeerID: "better", Online: true},
	}
	if a.routeCandidateReady(cfg, health, "better") {
		t.Fatal("first observation should not switch away from a healthy current route")
	}
	if !a.routeCandidateReady(cfg, health, "better") {
		t.Fatal("second consecutive observation should confirm the candidate")
	}
}

func TestRouteCandidateFailsOverImmediatelyWhenCurrentIsOffline(t *testing.T) {
	a := &Autopilot{}
	cfg := Config{SelectedExit: "current"}
	health := map[string]PeerHealth{
		"current": {PeerID: "current", Online: false},
		"backup":  {PeerID: "backup", Online: true},
	}
	if !a.routeCandidateReady(cfg, health, "backup") {
		t.Fatal("offline current route must not delay failover")
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 16 * time.Second}
	for failures, expected := range want {
		if got := retryDelay(failures); got != expected {
			t.Fatalf("retryDelay(%d)=%s, want %s", failures, got, expected)
		}
	}
}
