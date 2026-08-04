package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type HTTPProxy struct {
	node   *Node
	store  *Store
	logger *log.Logger
	mu     sync.Mutex
	ln     net.Listener
}

func NewHTTPProxy(node *Node, store *Store, logger *log.Logger) *HTTPProxy {
	return &HTTPProxy{node: node, store: store, logger: logger}
}

func (p *HTTPProxy) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		return nil
	}
	ln, err := net.Listen("tcp", p.store.Snapshot().HTTPListen)
	if err != nil {
		return err
	}
	p.ln = ln
	go func() { <-ctx.Done(); p.Stop() }()
	go p.acceptLoop(ln)
	return nil
}
func (p *HTTPProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		_ = p.ln.Close()
		p.ln = nil
	}
}
func (p *HTTPProxy) Running() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.ln != nil }
func (p *HTTPProxy) Address() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		return p.ln.Addr().String()
	}
	return fmt.Sprintf("disabled (%s)", p.store.Snapshot().HTTPListen)
}

func (p *HTTPProxy) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				p.logger.Printf("HTTP proxy accept: %v", err)
			}
			return
		}
		go p.handle(conn)
	}
}

func (p *HTTPProxy) handle(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(20 * time.Second))
	reader := bufio.NewReader(client)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	defer req.Body.Close()
	target := req.Host
	if !strings.Contains(target, ":") {
		if req.Method == http.MethodConnect {
			target += ":443"
		} else {
			target += ":80"
		}
	}
	cfg := p.store.Snapshot()
	peer, ok := cfg.Peers[cfg.SelectedExit]
	if cfg.SelectedExit == "" || !ok || !peer.Capabilities.UseExit {
		_, _ = client.Write([]byte("HTTP/1.1 503 Exit node unavailable\r\nConnection: close\r\n\r\n"))
		return
	}
	upstream, err := p.node.OpenStream(cfg.SelectedExit, "exit", target)
	if err != nil {
		_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
		return
	}
	defer upstream.Close()
	if req.Method == http.MethodConnect {
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	} else {
		req.RequestURI = ""
		req.URL.Scheme = ""
		req.URL.Host = ""
		if err := req.Write(upstream); err != nil {
			return
		}
	}
	_ = client.SetDeadline(time.Time{})
	if reader.Buffered() > 0 {
		buffered, _ := reader.Peek(reader.Buffered())
		_, _ = upstream.Write(buffered)
		_, _ = reader.Discard(len(buffered))
	}
	proxyDuplex(client, upstream)
}
