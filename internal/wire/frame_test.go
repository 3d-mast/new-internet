package wire

import (
	"bytes"
	"errors"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{0x42}, 32) }

func TestRoundTripAndReplay(t *testing.T) {
	h := Header{Type: 3, Flags: 7, SessionID: 0x11223344, Sequence: 9}
	frame, err := Seal(testKey(), h, []byte("hello esp32"))
	if err != nil { t.Fatal(err) }
	var guard ReplayGuard
	gotH, got, err := Open(testKey(), frame, &guard)
	if err != nil { t.Fatal(err) }
	if gotH != h || string(got) != "hello esp32" { t.Fatalf("roundtrip mismatch: %#v %q", gotH, got) }
	if _, _, err := Open(testKey(), frame, &guard); !errors.Is(err, ErrReplay) { t.Fatalf("expected replay error, got %v", err) }
}

func TestTamperRejected(t *testing.T) {
	frame, err := Seal(testKey(), Header{SessionID: 1, Sequence: 1}, []byte("secret"))
	if err != nil { t.Fatal(err) }
	frame[len(frame)-1] ^= 1
	if _, _, err := Open(testKey(), frame, nil); err == nil { t.Fatal("tampered frame accepted") }
}

func TestBoundedPayload(t *testing.T) {
	_, err := Seal(testKey(), Header{}, make([]byte, MaxPayload+1))
	if !errors.Is(err, ErrOversize) { t.Fatalf("expected oversize, got %v", err) }
}
