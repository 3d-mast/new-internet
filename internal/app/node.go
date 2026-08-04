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

type Node struct {
	identity  *Identity
	store     *Store
	discovery *Discovery
	logger    *log.Logger
	listener  net.Listener
	closeOnce sync.Once
}

func NewNode(identity *Identity, store *Store, logger *log.Logger) *Node {
	return &Node{identity: identity, store: store, discovery: NewDiscovery(identity, store, logger), logger: logger}
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
	n.discovery.Start(ctx)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go n.acceptLoop()
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
		return writeFrame(conn, Response{OK: true, NodeID: n.identity.ID(), Name: n.store.Snapshot().NodeName, PublicKey: n.identity.PublicKeyString()})
	case "exit", "tunnel":
		return n.acceptDataStream(conn, hello)
	default:
		return reject(conn, errors.New("unknown purpose"))
	}
}

func (n *Node) verifyHello(conn *tls.Conn, hello Hello) error {
	if hello.Version != ProtocolVersion {
		return errors.New("protocol version mismatch")
	}
	if time.Since(time.Unix(hello.Timestamp, 0)) > 2*time.Minute || time.Until(time.Unix(hello.Timestamp, 0)) > 2*time.Minute {
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
	cfg := n.store.Snapshot()
	peer, ok := cfg.Peers[id]
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
			Endpoints: []string{endpointForRemote(conn.RemoteAddr(), hello.ListenPort)},
			Permissions: item.Permissions, AddedAt: time.Now(), LastSeen: time.Now(),
		}
		return nil
	})
	if err != nil {
		return reject(conn, err)
	}
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
	proxyDuplex(conn, upstream)
	return nil
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
		cfg.Invitations[code] = Invitation{Code: code, Permissions: perms, ExpiresAt: expires}
		return nil
	}); err != nil {
		return "", err
	}
	cfg := n.store.Snapshot()
	payload := invitePayload{Version: ProtocolVersion, NodeID: n.identity.ID(), Name: cfg.NodeName, PublicKey: n.identity.PublicKeyString(), Endpoints: n.advertisedEndpoints(), Code: code, ExpiresAt: expires, Permissions: perms}
	payload.Signature = n.identity.Sign(payload.signingBytes())
	raw, _ := json.Marshal(payload)
	return "myc1." + base64.RawURLEncoding.EncodeToString(raw), nil
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
		hello.ListenPort = endpointPort(n.store.Snapshot().Listen)
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
		return n.store.Update(func(cfg *Config) error {
			cfg.Peers[payload.NodeID] = Peer{ID: payload.NodeID, Name: payload.Name, PublicKey: payload.PublicKey, Endpoints: payload.Endpoints, Permissions: Permissions{}, AddedAt: time.Now(), LastSeen: time.Now()}
			return nil
		})
	}
	if lastErr == nil {
		lastErr = errors.New("invitation has no reachable endpoints")
	}
	return lastErr
}

func decodeInvite(token string) (invitePayload, error) {
	var payload invitePayload
	if !strings.HasPrefix(token, "myc1.") {
		return payload, errors.New("invalid invitation prefix")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "myc1."))
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
	cfg := n.store.Snapshot()
	peer, ok := cfg.Peers[peerID]
	if !ok {
		return nil, errors.New("unknown peer")
	}
	var lastErr error
	for _, endpoint := range peer.Endpoints {
		conn, err := n.dialEndpoint(endpoint, peer.PublicKey)
		if err != nil {
			lastErr = err
			continue
		}
		hello := n.makeHello(purpose, target, "")
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
		if !response.OK {
			_ = conn.Close()
			lastErr = errors.New(response.Error)
			continue
		}
		return conn, nil
	}
	if lastErr == nil {
		lastErr = errors.New("peer has no endpoints")
	}
	return nil, lastErr
}

func (n *Node) makeHello(purpose, target, code string) Hello {
	cfg := n.store.Snapshot()
	hello := Hello{Version: ProtocolVersion, NodeID: n.identity.ID(), Name: cfg.NodeName, PublicKey: n.identity.PublicKeyString(), Purpose: purpose, Target: target, InviteCode: code, ListenPort: endpointPort(cfg.Listen), Timestamp: time.Now().Unix()}
	hello.Signature = n.identity.Sign(hello.signingBytes())
	return hello
}

func (n *Node) dialEndpoint(endpoint, expectedKey string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	raw, err := dialer.Dial("tcp", endpoint)
	if err != nil {
		return nil, err
	}
	expected, err := parsePublicKey(expectedKey)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	tlsConn := tls.Client(raw, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{n.identity.Certificate()},
		InsecureSkipVerify: true,
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
	if err := tlsConn.Handshake(); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (n *Node) advertisedEndpoints() []string {
	cfg := n.store.Snapshot()
	port := endpointPort(cfg.Listen)
	set := map[string]struct{}{}
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
