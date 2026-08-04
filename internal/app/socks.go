package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

type SOCKSServer struct {
	node   *Node
	store  *Store
	logger *log.Logger
	events *EventLog
	mu     sync.Mutex
	ln     net.Listener
}

func NewSOCKSServer(node *Node, store *Store, logger *log.Logger, events *EventLog) *SOCKSServer {
	return &SOCKSServer{node: node, store: store, logger: logger, events: events}
}

func (s *SOCKSServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return nil
	}
	ln, err := net.Listen("tcp", s.store.Snapshot().SOCKSListen)
	if err != nil {
		return err
	}
	s.ln = ln
	go func() { <-ctx.Done(); s.Stop() }()
	go s.acceptLoop(ln)
	return nil
}

func (s *SOCKSServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
	}
}

func (s *SOCKSServer) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.logger.Printf("SOCKS accept: %v", err)
			}
			return
		}
		go s.handle(conn)
	}
}

func (s *SOCKSServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	target, err := socksHandshake(conn)
	if err != nil {
		return
	}
	cfg := s.store.Snapshot()
	peer, ok := cfg.Peers[cfg.SelectedExit]
	if cfg.SelectedExit == "" || !ok || !peer.Capabilities.UseExit {
		_ = socksReply(conn, 0x01)
		return
	}
	upstream, err := s.node.OpenStream(cfg.SelectedExit, "exit", target)
	if err != nil {
		_ = socksReply(conn, 0x05)
		return
	}
	defer upstream.Close()
	if err := socksReply(conn, 0x00); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	proxyDuplex(conn, upstream)
}

func socksHandshake(conn net.Conn) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 {
		return "", errors.New("invalid SOCKS version")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil || req[0] != 5 || req[1] != 1 {
		return "", errors.New("unsupported SOCKS request")
	}
	var host string
	switch req[3] {
	case 1:
		raw := make([]byte, 4)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		host = net.IP(raw).String()
	case 3:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return "", err
		}
		raw := make([]byte, int(size[0]))
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		host = string(raw)
	case 4:
		raw := make([]byte, 16)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		host = net.IP(raw).String()
	default:
		return "", errors.New("unsupported address type")
	}
	var portRaw [2]byte
	if _, err := io.ReadFull(conn, portRaw[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portRaw[:])))), nil
}

func socksReply(conn net.Conn, code byte) error {
	_, err := conn.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}

func (s *SOCKSServer) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return fmt.Sprintf("disabled (%s)", s.store.Snapshot().SOCKSListen)
}
func (s *SOCKSServer) Running() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.ln != nil }
