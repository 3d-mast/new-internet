package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func LoadStore(path string) (*Store, error) {
	cfg := defaultConfig()
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("decode config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	normalizeConfig(&cfg)
	s := &Store{path: path, cfg: cfg}
	if errors.Is(err, os.ErrNotExist) {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func defaultConfig() Config {
	host, _ := os.Hostname()
	if host == "" {
		host = "Mycelium node"
	}
	return Config{
		NodeName:    host,
		Listen:      fmt.Sprintf("0.0.0.0:%d", DefaultMeshPort),
		APIListen:   fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort),
		SOCKSListen: fmt.Sprintf("127.0.0.1:%d", DefaultSOCKSPort),
		Peers:       make(map[string]Peer),
		Invitations: make(map[string]Invitation),
	}
}

func normalizeConfig(cfg *Config) {
	if cfg.NodeName == "" {
		cfg.NodeName = "Mycelium node"
	}
	if cfg.Listen == "" {
		cfg.Listen = fmt.Sprintf("0.0.0.0:%d", DefaultMeshPort)
	}
	if cfg.APIListen == "" {
		cfg.APIListen = fmt.Sprintf("127.0.0.1:%d", DefaultAPIPort)
	}
	if cfg.SOCKSListen == "" {
		cfg.SOCKSListen = fmt.Sprintf("127.0.0.1:%d", DefaultSOCKSPort)
	}
	if cfg.Peers == nil {
		cfg.Peers = make(map[string]Peer)
	}
	if cfg.Invitations == nil {
		cfg.Invitations = make(map[string]Invitation)
	}
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, _ := json.Marshal(s.cfg)
	var copy Config
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.cfg); err != nil {
		return err
	}
	normalizeConfig(&s.cfg)
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
