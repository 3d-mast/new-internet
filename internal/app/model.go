package app

import "time"

const (
	ProtocolVersion  = "MYCELIUM/1"
	DefaultMeshPort  = 47831
	DefaultAPIPort   = 47832
	DefaultSOCKSPort = 1088
)

type Permissions struct {
	UseExit   bool `json:"use_exit"`
	AccessLAN bool `json:"access_lan"`
	Relay     bool `json:"relay"`
}

type Peer struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	PublicKey   string      `json:"public_key"`
	Endpoints   []string    `json:"endpoints"`
	Permissions Permissions `json:"permissions"`
	AddedAt     time.Time   `json:"added_at"`
	LastSeen    time.Time   `json:"last_seen"`
}

type Invitation struct {
	Code        string      `json:"code"`
	Permissions Permissions `json:"permissions"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Used        bool        `json:"used"`
}

type TunnelConfig struct {
	ID      string `json:"id"`
	PeerID  string `json:"peer_id"`
	Listen  string `json:"listen"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	NodeName     string                `json:"node_name"`
	Listen       string                `json:"listen"`
	APIListen    string                `json:"api_listen"`
	SOCKSListen  string                `json:"socks_listen"`
	SOCKSEnabled bool                  `json:"socks_enabled"`
	OfferExit    bool                  `json:"offer_exit"`
	SelectedExit string                `json:"selected_exit"`
	Peers        map[string]Peer       `json:"peers"`
	Invitations  map[string]Invitation `json:"invitations"`
	Tunnels      []TunnelConfig        `json:"tunnels"`
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
	Active bool   `json:"active"`
	Error  string `json:"error,omitempty"`
}
