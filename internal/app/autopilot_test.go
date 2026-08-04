package app

import "testing"

func TestSelectBestExitPrefersHealthyDirectRoute(t *testing.T) {
	cfg := Config{
		SelectedExit: "relay-exit",
		Peers: map[string]Peer{
			"direct-exit": {ID: "direct-exit", Capabilities: Permissions{UseExit: true}},
			"relay-exit":  {ID: "relay-exit", Capabilities: Permissions{UseExit: true}},
			"no-exit":     {ID: "no-exit", Capabilities: Permissions{}},
		},
	}
	health := map[string]PeerHealth{
		"direct-exit": {PeerID: "direct-exit", Online: true, LatencyMS: 80, Route: "direct"},
		"relay-exit":  {PeerID: "relay-exit", Online: true, LatencyMS: 20, Route: "relay:middle"},
		"no-exit":     {PeerID: "no-exit", Online: true, LatencyMS: 1, Route: "direct"},
	}
	id, selected := selectBestExit(cfg, health)
	if id != "direct-exit" {
		t.Fatalf("expected direct-exit, got %q (%+v)", id, selected)
	}
}

func TestSelectBestExitKeepsHealthyRouteWithHysteresis(t *testing.T) {
	cfg := Config{
		SelectedExit: "current",
		Peers: map[string]Peer{
			"current": {ID: "current", Capabilities: Permissions{UseExit: true}},
			"other":   {ID: "other", Capabilities: Permissions{UseExit: true}},
		},
	}
	health := map[string]PeerHealth{
		"current": {PeerID: "current", Online: true, LatencyMS: 50, Route: "direct"},
		"other":   {PeerID: "other", Online: true, LatencyMS: 40, Route: "direct"},
	}
	id, _ := selectBestExit(cfg, health)
	if id != "current" {
		t.Fatalf("expected hysteresis to keep current route, got %q", id)
	}
}

func TestSelectBestExitReturnsEmptyWhenNoUsableExit(t *testing.T) {
	cfg := Config{Peers: map[string]Peer{
		"offline": {ID: "offline", Capabilities: Permissions{UseExit: true}},
		"online":  {ID: "online", Capabilities: Permissions{}},
	}}
	health := map[string]PeerHealth{
		"offline": {PeerID: "offline", Online: false},
		"online":  {PeerID: "online", Online: true, LatencyMS: 1, Route: "direct"},
	}
	id, _ := selectBestExit(cfg, health)
	if id != "" {
		t.Fatalf("expected no exit, got %q", id)
	}
}
