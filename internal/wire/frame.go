package wire

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Magic        uint32 = 0x424f5231 // BOR1
	Version      byte   = 1
	HeaderSize          = 20
	TagSize             = 16
	MaxPayload          = 1200
)

var (
	ErrShortFrame  = errors.New("boreal: short frame")
	ErrMagic       = errors.New("boreal: invalid magic")
	ErrVersion     = errors.New("boreal: unsupported version")
	ErrOversize    = errors.New("boreal: payload exceeds MTU")
	ErrLength      = errors.New("boreal: invalid frame length")
	ErrReplay      = errors.New("boreal: replayed or stale frame")
)

type Header struct {
	Type      byte
	Flags     uint16
	SessionID uint32
	Sequence  uint64
}

type ReplayGuard struct {
	SessionID uint32
	Highest   uint64
	seen      bool
}

func (r *ReplayGuard) Accept(session uint32, sequence uint64) error {
	if !r.seen || session != r.SessionID {
		r.SessionID, r.Highest, r.seen = session, sequence, true
		return nil
	}
	if sequence <= r.Highest {
		return ErrReplay
	}
	r.Highest = sequence
	return nil
}

func Seal(key []byte, h Header, payload []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("boreal: AES-256 key must be 32 bytes")
	}
	if len(payload) > MaxPayload {
		return nil, ErrOversize
	}
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }

	out := make([]byte, HeaderSize, HeaderSize+len(payload)+TagSize)
	binary.BigEndian.PutUint32(out[0:4], Magic)
	out[4] = Version
	out[5] = h.Type
	binary.BigEndian.PutUint16(out[6:8], h.Flags)
	binary.BigEndian.PutUint32(out[8:12], h.SessionID)
	binary.BigEndian.PutUint64(out[12:20], h.Sequence)
	nonce := makeNonce(h.SessionID, h.Sequence)
	return gcm.Seal(out, nonce[:], payload, out[:HeaderSize]), nil
}

func Open(key, frame []byte, guard *ReplayGuard) (Header, []byte, error) {
	var h Header
	if len(frame) < HeaderSize+TagSize { return h, nil, ErrShortFrame }
	if binary.BigEndian.Uint32(frame[0:4]) != Magic { return h, nil, ErrMagic }
	if frame[4] != Version { return h, nil, ErrVersion }
	if len(frame)-HeaderSize-TagSize > MaxPayload { return h, nil, ErrOversize }
	h.Type = frame[5]
	h.Flags = binary.BigEndian.Uint16(frame[6:8])
	h.SessionID = binary.BigEndian.Uint32(frame[8:12])
	h.Sequence = binary.BigEndian.Uint64(frame[12:20])

	block, err := aes.NewCipher(key)
	if err != nil { return h, nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return h, nil, err }
	nonce := makeNonce(h.SessionID, h.Sequence)
	plain, err := gcm.Open(nil, nonce[:], frame[HeaderSize:], frame[:HeaderSize])
	if err != nil { return h, nil, err }
	if len(plain) > MaxPayload { return h, nil, ErrLength }
	if guard != nil {
		if err := guard.Accept(h.SessionID, h.Sequence); err != nil { return h, nil, err }
	}
	return h, plain, nil
}

func makeNonce(session uint32, sequence uint64) [12]byte {
	var nonce [12]byte
	binary.BigEndian.PutUint32(nonce[0:4], session)
	binary.BigEndian.PutUint64(nonce[4:12], sequence)
	return nonce
}
