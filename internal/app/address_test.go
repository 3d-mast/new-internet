package app

import (
	"net/netip"
	"testing"
)

func TestDeriveNodeAddressesIsStableAndInReservedRanges(t *testing.T) {
	key := []byte("stable-ed25519-public-key-test-value")
	first := deriveNodeAddresses(key)
	second := deriveNodeAddresses(key)
	if first != second {
		t.Fatalf("address derivation is not stable: %#v != %#v", first, second)
	}

	v4, err := netip.ParseAddr(first.OverlayIPv4)
	if err != nil {
		t.Fatalf("invalid IPv4: %v", err)
	}
	if !netip.MustParsePrefix("100.64.0.0/10").Contains(v4) {
		t.Fatalf("IPv4 %s is outside 100.64.0.0/10", v4)
	}

	v6, err := netip.ParseAddr(first.OverlayIPv6)
	if err != nil {
		t.Fatalf("invalid IPv6: %v", err)
	}
	if !netip.MustParsePrefix("fd00::/8").Contains(v6) {
		t.Fatalf("IPv6 %s is outside fd00::/8", v6)
	}
	if first.InterfaceAssigned {
		t.Fatal("identity-only address must not claim an installed interface")
	}
}

func TestDeriveNodeAddressesChangesWithIdentity(t *testing.T) {
	one := deriveNodeAddresses([]byte("one"))
	two := deriveNodeAddresses([]byte("two"))
	if one.OverlayIPv4 == two.OverlayIPv4 && one.OverlayIPv6 == two.OverlayIPv6 {
		t.Fatal("different identities produced identical overlay addresses")
	}
}
