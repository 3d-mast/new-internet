package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

type invitePayload struct {
	Version     string      `json:"version"`
	NodeID      string      `json:"node_id"`
	Name        string      `json:"name"`
	PublicKey   string      `json:"public_key"`
	Endpoints   []string    `json:"endpoints"`
	Code        string      `json:"code"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Permissions Permissions `json:"permissions"`
	Signature   string      `json:"signature"`
}

func (p invitePayload) signingBytes() []byte {
	p.Signature = ""
	raw, _ := json.Marshal(p)
	return raw
}

type backhaulEntry struct {
	conn net.Conn
	done chan struct{}
	once sync.Once
}

type Node struct {
	identity         *Identity
	store            *Store
	discovery        *Discovery
	logger           *log.Logger
	events           *EventLog
	listener         net.Listener
	closeOnce        sync.Once
	healthMu         sync.RWMutex
	health           map[string]PeerHealth
	ctx              context.Context
	backhaulMu       sync.Mutex
	backhauls        map[string][]*backhaulEntry
	backhaulClientMu sync.Mutex
	backhaulClients  map[string]bool
}

func NewNode(identity *Identity, store *Store, logger *log.Logger, events *EventLog) *Node {
	return &Node{
		identity:        identity,
		store:           store,
		discovery:       NewDiscovery(identity, store, logger),
		logger:          logger,
		events:          events,
		health:          make(map[string]PeerHealth),
		backhauls:       make(map[string][]*backhaulEntry),
		backhaulClients: make(map[string]bool),
	}
}

func (n *Node) Start(ctx context.Context) error {
	cfg := n.store.Snapshot()
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{n.identity.Certificate()},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	listener, err := tls.Listen("tcp", cfg.Listen, tlsCfg)
	if err != nil {
		return err
	}
	n.listener = listener
	n.ctx = ctx
	n.discovery.Start(ctx)
	go func() { <-ctx.Done(); _ = listener.Close() }()
	go n.acceptLoop()
	go n.probeLoop(ctx)
	go n.backhaulSupervisor(ctx)
	n.events.Add("info", "node", "Сетевой узел запущен", "")
	return nil
}

func (n *Node) Close() {
	n.closeOnce.Do(func() {
		if n.listener != nil {
			_ = n.listener.Close()
		}
	})
}
func (n *Node) ID() string                   { return n.identity.ID() }
func (n *Node) Discovered() []DiscoveredNode { return n.discovery.Nodes() }

func (n *Node) Health() map[string]PeerHealth {
	n.healthMu.RLock()
	defer n.healthMu.RUnlock()
	out := make(map[string]PeerHealth, len(n.health))
	for id, item := range n.health {
		out[id] = item
	}
	return out
}

func (n *Node) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			n.logger.Printf("accept: %v", err)
			continue
		}
		go func() {
			if err := n.handleConn(conn); err != nil {
				n.logger.Printf("peer %s: %v", conn.RemoteAddr(), err)
			}
			_ = conn.Close()
		}()
	}
}

func (n *Node) handleConn(raw net.Conn) error {
	conn, ok := raw.(*tls.Conn)
	if !ok {
		return errors.New("non-TLS connection")
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if err := conn.Handshake(); err != nil {
		return err
	}
	var hello Hello
	if err := readFrame(conn, &hello); err != nil {
		return err
	}
	if err := n.verifyHello(conn, hello); err != nil {
		_ = writeFrame(conn, Response{OK: false, Error: err.Error()})
		return err
	}
	_ = conn.SetDeadline(time.Time{})

	switch hello.Purpose {
	case "join":
		return n.acceptJoin(conn, hello)
	case "control":
		if _, err := n.trustedPeer(hello.NodeID, hello.PublicKey); err != nil {
			return reject(conn, err)
		}
		n.markIncomingOnline(hello.NodeID)
		return writeFrame(conn, Response{OK: true, NodeID: n.identity.ID(), Name: n.store.Snapshot().NodeName, PublicKey: n.identity.PublicKeyString(), Route: "direct"})
	case "exit", "tunnel":
		return n.acceptDataStream(conn, hello)
	case "relay":
		return n.acceptRelay(conn, hello)
	case "backhaul":
		return n.acceptBackhaul(conn, hello)
	default:
		return reject(conn, errors.New("unknown purpose"))
	}
}

func (n *Node) verifyHello(conn *tls.Conn, hello Hello) error {
	if hello.Version != ProtocolVersion {
		return errors.New("protocol version mismatch")
	}
	stamp := time.Unix(hello.Timestamp, 0)
	if time.Since(stamp) > 2*time.Minute || time.Until(stamp) > 2*time.Minute {
		return errors.New("stale handshake")
	}
	pub, err := parsePublicKey(hello.PublicKey)
	if err != nil || nodeID(pub) != hello.NodeID {
		return errors.New("identity mismatch")
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return errors.New("missing peer certificate")
	}
	certPub, ok := state.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok || !certPub.Equal(pub) {
		return errors.New("certificate identity mismatch")
	}
	if !verifySignature(hello.PublicKey, hello.signingBytes(), hello.Signature) {
		return errors.New("invalid handshake signature")
	}
	return nil
}

func (n *Node) trustedPeer(id, publicKey string) (Peer, error) {
	peer, ok := n.store.Snapshot().Peers[id]
	if !ok || peer.PublicKey != publicKey {
		return Peer{}, errors.New("peer is not trusted")
	}
	return peer, nil
}

func (n *Node) acceptJoin(conn net.Conn, hello Hello) error {
	var invitation Invitation
	err := n.store.Update(func(cfg *Config) error {
		item, ok := cfg.Invitations[hello.InviteCode]
		if !ok || item.Used || time.Now().After(item.ExpiresAt) {
			return errors.New("invitation is invalid or expired")
		}
		item.Used = true
		cfg.Invitations[hello.InviteCode] = item
		invitation = item
		cfg.Peers[hello.NodeID] = Peer{
			ID: hello.NodeID, Name: hello.Name, PublicKey: hello.PublicKey,
			Endpoints:   []string{endpointForRemote(conn.RemoteAddr(), hello.ListenPort)},
			Permissions: item.Permissions, Capabilities: Permissions{}, AddedAt: time.Now(), LastSeen: time.Now(),
		}
		return nil
	})
	if err != nil {
		return reject(conn, err)
	}
	n.events.Add("success", "join", "Новый узел принят по одноразовому приглашению", hello.NodeID)
	cfg := n.store.Snapshot()
	return writeFrame(conn, Response{OK: true, NodeID: n.identity.ID(), Name: cfg.NodeName, PublicKey: n.identity.PublicKeyString(), Permissions: invitation.Permissions})
}

func (n *Node) acceptDataStream(conn net.Conn, hello Hello) error {
	peer, err := n.trustedPeer(hello.NodeID, hello.PublicKey)
	if err != nil {
		return reject(conn, err)
	}
	cfg := n.store.Snapshot()
	if hello.Purpose == "exit" {
		if !cfg.OfferExit || !peer.Permissions.UseExit {
			return reject(conn, errors.New("exit permission denied"))
		}
		if err := validateTarget(hello.Target, peer.Permissions.AccessLAN); err != nil {
			return reject(conn, err)
		}
	} else if !peer.Permissions.AccessLAN {
		return reject(conn, errors.New("LAN permission denied"))
	}
	upstream, err := net.DialTimeout("tcp", hello.Target, 10*time.Second)
	if err != nil {
		return reject(conn, fmt.Errorf("target unavailable: %w", err))
	}
	defer upstream.Close()
	if err := writeFrame(conn, Response{OK: true}); err != nil {
		return err
	}
	n.markIncomingOnline(hello.NodeID)
	proxyDuplex(conn, upstream)
	return nil
}

// acceptRelay forwards an opaque inner TLS stream. The relay can see metadata,
// but cannot decrypt the nested connection between origin and destination.
func (n *Node) acceptRelay(conn net.Conn, hello Hello) error {
	requester, err := n.trustedPeer(hello.NodeID, hello.PublicKey)
	if err != nil {
		return reject(conn, err)
	}
	if !requester.Permissions.Relay {
		return reject(conn, errors.New("relay permission denied"))
	}
	destination, ok := n.store.Snapshot().Peers[hello.Target]
	if !ok {
		return reject(conn, errors.New("relay destination is unknown"))
	}

	raw, endpoint, directErr := n.dialRawPeer(destination)
	if directErr == nil {
		defer raw.Close()
		if err := writeFrame(conn, Response{OK: true, Route: "relay-direct:" + endpoint}); err != nil {
			return err
		}
		n.events.Add("info", "relay", "Зашифрованный поток передан через прямой relay", hello.NodeID)
		proxyDuplex(conn, raw)
		return nil
	}

	entry := n.claimBackhaul(destination.ID)
	if entry == nil {
		return reject(conn, fmt.Errorf("relay destination unavailable: %w", directErr))
	}
	if err := writeFrame(entry.conn, BackhaulCommand{Open: true}); err != nil {
		entry.once.Do(func() { close(entry.done) })
		return reject(conn, errors.New("reverse relay channel failed"))
	}
	if err := writeFrame(conn, Response{OK: true, Route: "relay-backhaul:" + destination.ID}); err != nil {
		entry.once.Do(func() { close(entry.done) })
		return err
	}
	n.events.Add("info", "relay", "Зашифрованный поток передан через обратный NAT-канал", hello.NodeID)
	proxyDuplex(conn, entry.conn)
	entry.once.Do(func() { close(entry.done) })
	return nil
}

func (n *Node) acceptBackhaul(conn net.Conn, hello Hello) error {
	peer, err := n.trustedPeer(hello.NodeID, hello.PublicKey)
	if err != nil {
		return reject(conn, err)
	}
	if !peer.Permissions.Relay {
		return reject(conn, errors.New("backhaul permission denied"))
	}
	if err := writeFrame(conn, Response{OK: true, Route: "backhaul-ready"}); err != nil {
		return err
	}
	entry := &backhaulEntry{conn: conn, done: make(chan struct{})}
	n.backhaulMu.Lock()
	n.backhauls[hello.NodeID] = append(n.backhauls[hello.NodeID], entry)
	n.backhaulMu.Unlock()
	n.events.Add("info", "backhaul", "Узел зарегистрировал обратный канал через NAT", hello.NodeID)
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case <-entry.done:
		return nil
	case <-timer.C:
		n.removeBackhaul(hello.NodeID, entry)
		return nil
	case <-n.ctx.Done():
		n.removeBackhaul(hello.NodeID, entry)
		return nil
	}
}

func (n *Node) claimBackhaul(peerID string) *backhaulEntry {
	n.backhaulMu.Lock()
	defer n.backhaulMu.Unlock()
	items := n.backhauls[peerID]
	if len(items) == 0 {
		return nil
	}
	entry := items[0]
	if len(items) == 1 {
		delete(n.backhauls, peerID)
	} else {
		n.backhauls[peerID] = items[1:]
	}
	return entry
}

func (n *Node) removeBackhaul(peerID string, target *backhaulEntry) {
	n.backhaulMu.Lock()
	defer n.backhaulMu.Unlock()
	items := n.backhauls[peerID]
	out := items[:0]
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		delete(n.backhauls, peerID)
	} else {
		n.backhauls[peerID] = out
	}
}

func (n *Node) backhaulSupervisor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		n.ensureBackhaulClients(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Node) ensureBackhaulClients(ctx context.Context) {
	for id, peer := range n.store.Snapshot().Peers {
		if !peer.Capabilities.Relay {
			continue
		}
		n.backhaulClientMu.Lock()
		if n.backhaulClients[id] {
			n.backhaulClientMu.Unlock()
			continue
		}
		n.backhaulClients[id] = true
		n.backhaulClientMu.Unlock()
		go n.backhaulClientLoop(ctx, id)
	}
}

func (n *Node) backhaulClientLoop(ctx context.Context, relayID string) {
	defer func() { n.backhaulClientMu.Lock(); delete(n.backhaulClients, relayID); n.backhaulClientMu.Unlock() }()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		relay, ok := n.store.Snapshot().Peers[relayID]
		if !ok || !relay.Capabilities.Relay {
			return
		}
		conn, _, err := n.openDirect(relay, "backhaul", "")
		if err != nil {
			if !sleepContext(ctx, 3*time.Second) {
				return
			}
			continue
		}
		var command BackhaulCommand
		_ = conn.SetDeadline(time.Now().Add(50 * time.Second))
		err = readFrame(conn, &command)
		_ = conn.SetDeadline(time.Time{})
		if err == nil && command.Open {
			inner := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{n.identity.Certificate()}, ClientAuth: tls.RequireAnyClientCert})
			_ = n.handleConn(inner)
			_ = inner.Close()
		} else {
			_ = conn.Close()
		}
		if !sleepContext(ctx, 250*time.Millisecond) {
			return
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func reject(conn net.Conn, err error) error {
	_ = writeFrame(conn, Response{OK: false, Error: err.Error()})
	return err
}

func (n *Node) CreateInvite(perms Permissions, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 15 * time.Minute
	}
	codeBytes := make([]byte, 24)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(codeBytes)
	expires := time.Now().Add(ttl)
	if err := n.store.Update(func(cfg *Config) error {
		cfg.Invitations[code] = Invitation{Code: code, Permissions: perms, ExpiresAt: expires, CreatedAt: time.Now()}
		return nil
	}); err != nil {
		return "", err
	}
	cfg := n.store.Snapshot()
	payload := invitePayload{Version: ProtocolVersion, NodeID: n.identity.ID(), Name: cfg.NodeName, PublicKey: n.identity.PublicKeyString(), Endpoints: n.advertisedEndpoints(), Code: code, ExpiresAt: expires, Permissions: perms}
	payload.Signature = n.identity.Sign(payload.signingBytes())
	raw, _ := json.Marshal(payload)
	n.events.Add("info", "invite", "Создано одноразовое приглашение", "")
	return InvitePrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (n *Node) RevokeInvite(code string) error {
	return n.store.Update(func(cfg *Config) error {
		if _, ok := cfg.Invitations[code]; !ok {
			return errors.New("invitation not found")
		}
		delete(cfg.Invitations, code)
		return nil
	})
}

func (n *Node) JoinInvite(token string) error {
	payload, err := decodeInvite(token)
	if err != nil {
		return err
	}
	var lastErr error
	for _, endpoint := range payload.Endpoints {
		conn, err := n.dialEndpoint(endpoint, payload.PublicKey)
		if err != nil {
			lastErr = err
			continue
		}
		hello := n.makeHello("join", "", payload.Code)
		if err := writeFrame(conn, hello); err != nil {
			_ = conn.Close()
			lastErr = err
			continue
		}
		var response Response
		if err := readFrame(conn, &response); err != nil {
			_ = conn.Close()
			lastErr = err
			continue
		}
		_ = conn.Close()
		if !response.OK {
			lastErr = errors.New(response.Error)
			continue
		}
		if response.NodeID != payload.NodeID || response.PublicKey != payload.PublicKey {
			return errors.New("inviter identity changed")
		}
		err = n.store.Update(func(cfg *Config) error {
			cfg.Peers[payload.NodeID] = Peer{ID: payload.NodeID, Name: payload.Name, PublicKey: payload.PublicKey, Endpoints: payload.Endpoints, Permissions: Permissions{}, Capabilities: response.Permissions, AddedAt: time.Now(), LastSeen: time.Now()}
			return nil
		})
		if err == nil {
			n.events.Add("success", "join", "Подключение к узлу установлено", payload.NodeID)
		}
		return err
	}
	if lastErr == nil {
		lastErr = errors.New("invitation has no reachable endpoints")
	}
	return lastErr
}

func decodeInvite(token string) (invitePayload, error) {
	var payload invitePayload
	if !strings.HasPrefix(token, InvitePrefix) {
		return payload, errors.New("invalid invitation prefix")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, InvitePrefix))
	if err != nil || json.Unmarshal(raw, &payload) != nil {
		return payload, errors.New("invalid invitation")
	}
	if payload.Version != ProtocolVersion || time.Now().After(payload.ExpiresAt) {
		return payload, errors.New("invitation expired or incompatible")
	}
	if nodeIDMust(payload.PublicKey) != payload.NodeID || !verifySignature(payload.PublicKey, payload.signingBytes(), payload.Signature) {
		return payload, errors.New("invalid invitation signature")
	}
	return payload, nil
}

func (n *Node) OpenStream(peerID, purpose, target string) (net.Conn, error) {
	conn, _, _, err := n.openStreamWithRoute(peerID, purpose, target)
	return conn, err
}

func (n *Node) openStreamWithRoute(peerID, purpose, target string) (net.Conn, string, string, error) {
	cfg := n.store.Snapshot()
	peer, ok := cfg.Peers[peerID]
	if !ok {
		return nil, "", "", errors.New("unknown peer")
	}
	if conn, endpoint, err := n.openDirect(peer, purpose, target); err == nil {
		return conn, "direct", endpoint, nil
	} else if !cfg.AutoRelay {
		return nil, "", "", err
	}
	var lastErr error
	for _, relay := range n.relayCandidates(peerID) {
		conn, endpoint, err := n.openViaRelay(relay, peer, purpose, target)
		if err == nil {
			return conn, "relay:" + relay.ID, endpoint, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("peer is unreachable directly and no relay is available")
	}
	return nil, "", "", lastErr
}

func (n *Node) openDirect(peer Peer, purpose, target string) (net.Conn, string, error) {
	var lastErr error
	for _, endpoint := range peer.Endpoints {
		conn, err := n.dialEndpoint(endpoint, peer.PublicKey)
		if err != nil {
			lastErr = err
			continue
		}
		if err := n.finishOpen(conn, purpose, target); err != nil {
			_ = conn.Close()
			lastErr = err
			continue
		}
		return conn, endpoint, nil
	}
	if lastErr == nil {
		lastErr = errors.New("peer has no endpoints")
	}
	return nil, "", lastErr
}

func (n *Node) openViaRelay(relay, destination Peer, purpose, target string) (net.Conn, string, error) {
	outer, relayEndpoint, err := n.openDirect(relay, "relay", destination.ID)
	if err != nil {
		return nil, "", err
	}
	inner, err := n.tlsClientOnConn(outer, destination.PublicKey)
	if err != nil {
		_ = outer.Close()
		return nil, "", err
	}
	if err := n.finishOpen(inner, purpose, target); err != nil {
		_ = inner.Close()
		return nil, "", err
	}
	return inner, relayEndpoint, nil
}

func (n *Node) finishOpen(conn net.Conn, purpose, target string) error {
	hello := n.makeHello(purpose, target, "")
	if err := writeFrame(conn, hello); err != nil {
		return err
	}
	var response Response
	if err := readFrame(conn, &response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	return nil
}

func (n *Node) relayCandidates(destinationID string) []Peer {
	cfg := n.store.Snapshot()
	out := make([]Peer, 0)
	for id, peer := range cfg.Peers {
		if id != destinationID && peer.Capabilities.Relay {
			out = append(out, peer)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		hi, hj := n.healthFor(out[i].ID), n.healthFor(out[j].ID)
		if hi.Online != hj.Online {
			return hi.Online
		}
		return hi.LatencyMS < hj.LatencyMS
	})
	return out
}

func (n *Node) dialRawPeer(peer Peer) (net.Conn, string, error) {
	var lastErr error
	for _, endpoint := range peer.Endpoints {
		conn, err := net.DialTimeout("tcp", endpoint, 8*time.Second)
		if err == nil {
			return conn, endpoint, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("peer has no endpoints")
	}
	return nil, "", lastErr
}

func (n *Node) makeHello(purpose, target, code string) Hello {
	cfg := n.store.Snapshot()
	hello := Hello{Version: ProtocolVersion, NodeID: n.identity.ID(), Name: cfg.NodeName, PublicKey: n.identity.PublicKeyString(), Purpose: purpose, Target: target, InviteCode: code, ListenPort: endpointPort(cfg.Listen), Timestamp: time.Now().Unix()}
	hello.Signature = n.identity.Sign(hello.signingBytes())
	return hello
}

func (n *Node) dialEndpoint(endpoint, expectedKey string) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: 8 * time.Second}).Dial("tcp", endpoint)
	if err != nil {
		return nil, err
	}
	conn, err := n.tlsClientOnConn(raw, expectedKey)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

func (n *Node) tlsClientOnConn(raw net.Conn, expectedKey string) (*tls.Conn, error) {
	expected, err := parsePublicKey(expectedKey)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(raw, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{n.identity.Certificate()},
		InsecureSkipVerify: true, // Replaced by exact Ed25519 key pinning below.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("missing server certificate")
			}
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			pub, ok := cert.PublicKey.(ed25519.PublicKey)
			if !ok || !pub.Equal(expected) {
				return errors.New("server identity mismatch")
			}
			return nil
		},
	})
	_ = tlsConn.SetDeadline(time.Now().Add(12 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	_ = tlsConn.SetDeadline(time.Time{})
	return tlsConn, nil
}

func (n *Node) advertisedEndpoints() []string {
	cfg := n.store.Snapshot()
	port := endpointPort(cfg.Listen)
	set := map[string]struct{}{}
	for _, endpoint := range cfg.ManualEndpoints {
		set[endpoint] = struct{}{}
	}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil || !prefix.Addr().Is4() {
				continue
			}
			set[net.JoinHostPort(prefix.Addr().String(), fmt.Sprint(port))] = struct{}{}
		}
	}
	set[net.JoinHostPort("127.0.0.1", fmt.Sprint(port))] = struct{}{}
	out := make([]string, 0, len(set))
	for endpoint := range set {
		out = append(out, endpoint)
	}
	sort.Strings(out)
	return out
}

func (n *Node) RemovePeer(peerID string) error {
	err := n.store.Update(func(cfg *Config) error {
		if _, ok := cfg.Peers[peerID]; !ok {
			return errors.New("peer not found")
		}
		delete(cfg.Peers, peerID)
		if cfg.SelectedExit == peerID {
			cfg.SelectedExit = ""
			cfg.ProxyEnabled = false
			cfg.SystemProxy = false
		}
		out := cfg.Tunnels[:0]
		for _, tunnel := range cfg.Tunnels {
			if tunnel.PeerID != peerID {
				out = append(out, tunnel)
			}
		}
		cfg.Tunnels = out
		return nil
	})
	if err == nil {
		n.healthMu.Lock()
		delete(n.health, peerID)
		n.healthMu.Unlock()
		n.events.Add("warning", "peer", "Доверие к узлу отозвано", peerID)
	}
	return err
}

func (n *Node) UpdatePeer(peerID, name string, endpoints []string, permissions Permissions) error {
	return n.store.Update(func(cfg *Config) error {
		peer, ok := cfg.Peers[peerID]
		if !ok {
			return errors.New("peer not found")
		}
		if strings.TrimSpace(name) != "" {
			peer.Name = strings.TrimSpace(name)
		}
		if endpoints != nil {
			peer.Endpoints = cleanStrings(endpoints)
		}
		peer.Permissions = permissions
		cfg.Peers[peerID] = peer
		return nil
	})
}

func (n *Node) probeLoop(ctx context.Context) {
	ticker := time.NewTicker(DefaultProbeEvery)
	defer ticker.Stop()
	n.ProbeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.ProbeAll(ctx)
		}
	}
}

func (n *Node) ProbeAll(ctx context.Context) {
	cfg := n.store.Snapshot()
	for id := range cfg.Peers {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n.probeOne(id)
	}
}

func (n *Node) probeOne(peerID string) PeerHealth {
	start := time.Now()
	conn, route, endpoint, err := n.openStreamWithRoute(peerID, "control", "")
	if conn != nil {
		_ = conn.Close()
	}
	previous := n.healthFor(peerID)
	item := PeerHealth{PeerID: peerID, CheckedAt: time.Now(), Route: route, Endpoint: endpoint}
	if err == nil {
		item.Online = true
		item.LatencyMS = time.Since(start).Milliseconds()
		item.LastOnline = item.CheckedAt
		_ = n.store.Update(func(cfg *Config) error {
			peer, ok := cfg.Peers[peerID]
			if !ok {
				return nil
			}
			peer.LastSeen = item.CheckedAt
			for _, found := range n.Discovered() {
				if found.ID == peerID {
					peer.Endpoints = cleanStrings(append([]string{found.Endpoint}, peer.Endpoints...))
				}
			}
			cfg.Peers[peerID] = peer
			return nil
		})
		if !previous.Online {
			n.events.Add("success", "peer-online", "Узел снова доступен", peerID)
		}
	} else {
		item.Error = err.Error()
		item.LastOnline = previous.LastOnline
		if previous.Online {
			n.events.Add("warning", "peer-offline", "Связь с узлом потеряна", peerID)
		}
	}
	n.healthMu.Lock()
	n.health[peerID] = item
	n.healthMu.Unlock()
	return item
}

func (n *Node) Probe(peerID string) PeerHealth { return n.probeOne(peerID) }

func (n *Node) healthFor(peerID string) PeerHealth {
	n.healthMu.RLock()
	defer n.healthMu.RUnlock()
	return n.health[peerID]
}

func (n *Node) markIncomingOnline(peerID string) {
	n.healthMu.Lock()
	item := n.health[peerID]
	item.PeerID = peerID
	item.Online = true
	item.CheckedAt = time.Now()
	item.LastOnline = item.CheckedAt
	if item.Route == "" {
		item.Route = "incoming"
	}
	n.health[peerID] = item
	n.healthMu.Unlock()
}

func validateTarget(target string, allowPrivate bool) error {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return errors.New("target must be host:port")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return errors.New("target cannot be resolved")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsPrivate() || addr.IsUnspecified() {
			if !allowPrivate {
				return errors.New("private target requires LAN permission")
			}
		}
	}
	return nil
}

func proxyDuplex(a, b net.Conn) {
	done := make(chan struct{}, 2)
	copyOne := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyOne(a, b)
	go copyOne(b, a)
	<-done
}
