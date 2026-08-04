package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestNodeIDStable(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first := nodeID(pub)
	second := nodeID(pub)
	if first == "" || first != second {
		t.Fatalf("unstable node id: %q %q", first, second)
	}
}

func TestInviteSignature(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := newIdentity(private)
	if err != nil {
		t.Fatal(err)
	}
	payload := invitePayload{Version: ProtocolVersion, NodeID: identity.ID(), PublicKey: identity.PublicKeyString()}
	payload.Signature = identity.Sign(payload.signingBytes())
	if !verifySignature(payload.PublicKey, payload.signingBytes(), payload.Signature) {
		t.Fatal("signature rejected")
	}
}
