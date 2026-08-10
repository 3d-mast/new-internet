package app

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// Control frames are intentionally small. Keeping a tight bound limits memory
// amplification from an authenticated-but-malicious or misconfigured peer while
// leaving plenty of room for current and future handshake metadata.
const maxFrameSize = 64 << 10

type Hello struct {
	Version    string `json:"version"`
	NodeID     string `json:"node_id"`
	Name       string `json:"name"`
	PublicKey  string `json:"public_key"`
	Purpose    string `json:"purpose"`
	Target     string `json:"target,omitempty"`
	InviteCode string `json:"invite_code,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Signature  string `json:"signature"`
}

type BackhaulCommand struct {
	Open bool `json:"open"`
}

type Response struct {
	OK          bool        `json:"ok"`
	Error       string      `json:"error,omitempty"`
	NodeID      string      `json:"node_id,omitempty"`
	Name        string      `json:"name,omitempty"`
	PublicKey   string      `json:"public_key,omitempty"`
	Permissions Permissions `json:"permissions,omitempty"`
	Route       string      `json:"route,omitempty"`
}

func (h Hello) signingBytes() []byte {
	h.Signature = ""
	raw, _ := json.Marshal(h)
	return raw
}

func writeFrame(w io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maxFrameSize {
		return errors.New("frame too large")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(raw)))
	buffers := netBuffers(size[:], raw)
	_, err = buffers.WriteTo(w)
	return err
}

// netBuffers is kept as a tiny wrapper so framing tests can stay independent
// from concrete network connections while production writes use a single
// vectored operation where the platform supports it.
func netBuffers(parts ...[]byte) bufferWriter { return bufferWriter(parts) }

type bufferWriter [][]byte

func (b bufferWriter) WriteTo(w io.Writer) (int64, error) {
	var total int64
	for _, part := range b {
		n, err := w.Write(part)
		total += int64(n)
		if err != nil {
			return total, err
		}
		if n != len(part) {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func readFrame(r io.Reader, value any) error {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > maxFrameSize {
		return errors.New("invalid frame size")
	}
	raw := make([]byte, n)
	if _, err := io.ReadFull(r, raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	// Unknown JSON fields remain accepted for forward/backward compatibility,
	// but a frame must contain exactly one JSON value with no appended payload.
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("frame contains trailing JSON value")
		}
		return errors.New("frame contains trailing data")
	}
	return nil
}
