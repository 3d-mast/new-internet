package app

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	want := Response{OK: true, NodeID: "node", Route: "direct"}
	if err := writeFrame(&buffer, want); err != nil {
		t.Fatal(err)
	}
	var got Response
	if err := readFrame(&buffer, &got); err != nil {
		t.Fatal(err)
	}
	if got.OK != want.OK || got.NodeID != want.NodeID || got.Route != want.Route {
		t.Fatalf("unexpected frame: %+v", got)
	}
}

func TestReadFrameRejectsOversizeBeforeAllocation(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(maxFrameSize+1))
	var got Response
	if err := readFrame(bytes.NewReader(header[:]), &got); err == nil || !strings.Contains(err.Error(), "invalid frame size") {
		t.Fatalf("expected size rejection, got %v", err)
	}
}

func TestReadFrameAcceptsUnknownFieldsForCompatibility(t *testing.T) {
	raw := []byte(`{"ok":true,"future_field":"preserved-by-newer-peer"}`)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	buffer := append(header[:], raw...)
	var got Response
	if err := readFrame(bytes.NewReader(buffer), &got); err != nil {
		t.Fatalf("forward-compatible field should be ignored: %v", err)
	}
	if !got.OK {
		t.Fatal("expected decoded response")
	}
}

func TestReadFrameRejectsTrailingJSONValue(t *testing.T) {
	raw := []byte(`{"ok":true}{"ok":false}`)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(raw)))
	buffer := append(header[:], raw...)
	var got Response
	if err := readFrame(bytes.NewReader(buffer), &got); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}
