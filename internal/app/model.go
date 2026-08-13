package app

import "time"

const (
	ProductName       = "Boreal"
	ReleaseVersion    = "6.0.0"
	ProtocolVersion   = "BOREAL/1"
	InvitePrefix      = "bor1."
	CurrentConfig     = 6
	DefaultMeshPort   = 47831
	DefaultAPIPort    = 47832
	DefaultSOCKSPort  = 1088
	DefaultHTTPPort   = 1089
	DefaultProbeEvery = 8 * time.Second
)

type Permissions struct {
	UseExit   bool `json:"use_exit"`
	AccessLAN bool `json:"access_lan"`
	Relay     bool `json:"relay"`
}

type Peer struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	PublicKey    string      `json:"public_key"`
	Endpoints    []string    `json:"endpoints"`
	Permissions  Permissions `json:"permissions"`
	Capabilities Permissions `json:"capabilities"`
	AddedAt      time.Time   `json:"added_at"`
	LastSeen     time.Time   `json:"last_seen"`
}

type PeerHealth struct {
	PeerID     string    `json:"peer_id"`
	Online     bool      `json:"online"`
	LatencyMS  int64     `json:"latency_ms"`
	Route      string    `json:"route"`
	Endpoint   string    `json:"endpoint,omitempty"`
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	LastOnline time.Time `json:"last_online,omitempty"`
}

type Invitation struct {
	Code        string      `json:"code"`
	Permissions Permissions `json:"permissions"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Used        bool        `json:"used"`
	CreatedAt   time.Time   `json:"created_at"`
}

type TunnelConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PeerID  string `json:"peer_id"`
	Listen  string `json:"listen"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	ConfigVersion   int                   `json:"config_version"`
	NodeName        string                `json:"node_name"`
	Listen          string                `json:"listen"`
	APIListen       string                `json:"api_listen"`
	SOCKSListen     string                `json:"socks_listen"`
	HTTPListen      string                `json:"http_listen"`
	Autopilot       bool                  `json:"autopilot"`
	ProxyEnabled    bool                  `json:"proxy_enabled"`
	SystemProxy     bool                  `json:"system_proxy"`
	OfferExit       bool                  `json:"offer_exit"`
	SelectedExit    string                `json:"selected_exit"`
	AutoRelay       bool                  `json:"auto_relay"`
	ServerMode      bool                  `json:"server_mode"`
	ManualEndpoints []string              `json:"manual_endpoints"`
	Peers           map[string]Peer       `json:"peers"`
	Invitations     map[string]Invitation `json:"invitations"`
	Tunnels         []TunnelConfig        `json:"tunnels"`
}

type AutopilotStatus struct {
	Enabled      bool   `json:"enabled"`
	Active       bool   `json:"active"`
	SelectedExit string `json:"selected_exit,omitempty"`
	Route        string `json:"route,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
	Reason       string `json:"reason"`
}

type DiscoveredNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"`
	Endpoint  string    `json:"endpoint"`
	SeenAt    time.Time `json:"seen_at"`
}

type TunnelStatus struct {
	TunnelConfig
	Active      bool   `json:"active"`
	Connections int64  `json:"connections"`
	Error       string `json:"error,omitempty"`
}

type Event struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	PeerID  string    `json:"peer_id,omitempty"`
}

type DiagnosticItem struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Details string `json:"details"`
}
