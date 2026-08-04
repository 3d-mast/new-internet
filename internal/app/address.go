package app

import (
	"crypto/sha256"
	"net/netip"
)

// NodeAddressInfo separates the stable overlay identity from an address that is
// actually installed on an operating-system network interface. Reef 4 does not
// pretend that a TUN interface exists before the dataplane is implemented.
type NodeAddressInfo struct {
	OverlayIPv4       string `json:"overlay_ipv4"`
	OverlayIPv6       string `json:"overlay_ipv6"`
	InterfaceAssigned bool   `json:"interface_assigned"`
	Mode              string `json:"mode"`
	Note              string `json:"note"`
}

func (a *App) NodeAddresses() NodeAddressInfo {
	return deriveNodeAddresses(a.identity.public)
}

func deriveNodeAddresses(publicKey []byte) NodeAddressInfo {
	sum := sha256.Sum256(publicKey)

	// 100.64.0.0/10 is reserved for shared address space and avoids collision
	// with ordinary RFC1918 LAN ranges in the UI. It is currently an overlay
	// identity only, not a configured Windows interface address.
	v4 := netip.AddrFrom4([4]byte{
		100,
		64 + (sum[0] & 0x3f),
		sum[1],
		1 + (sum[2] % 254),
	})

	var rawV6 [16]byte
	rawV6[0] = 0xfd
	copy(rawV6[1:], sum[:15])
	v6 := netip.AddrFrom16(rawV6)

	return NodeAddressInfo{
		OverlayIPv4:       v4.String(),
		OverlayIPv6:       v6.String(),
		InterfaceAssigned: false,
		Mode:              "identity-only",
		Note:              "Логические адреса закреплены за ключом узла. Системный TUN/Wintun-интерфейс пока не установлен.",
	}
}
