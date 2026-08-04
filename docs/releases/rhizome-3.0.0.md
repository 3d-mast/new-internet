# Rhizome 3.0.0 Preview

Rhizome is the zero-touch successor to Lichen. The wire protocol remains compatible with LICHEN/2 so existing identities and trusted peers continue to work.

## Download

- Windows x64: `rhizome-windows-amd64.zip`
- Linux x64: `rhizome-linux-amd64.tar.gz`
- Checksums: `SHA256SUMS.txt`

## Highlights

- one-button autopilot for route selection and failover;
- automatic direct → relay → reverse backhaul routing;
- automatic exit-node selection with latency scoring and hysteresis;
- safe Windows system-proxy rollback when all exits are unavailable;
- headless `--server` mode for VPS and Docker;
- Ed25519 identity migration from Lichen;
- embedded GUI without Electron;
- TLS 1.3 and nested end-to-end TLS through relays.

## Verification

The release workflow runs Go tests, `go vet`, JavaScript syntax checks, Linux and Windows builds before publishing assets.

## Current limitations

Rhizome 3 is still a technical preview. It does not yet provide Wintun/TUN, UDP or QUIC dataplanes, generic IP packet routing, or guaranteed connectivity through networks that block all available transports.

## Attribution

Original project creator and concept author: Alexey Prokopchuk (Алексей Прокопчук).
Apache License 2.0; retain `LICENSE` and `NOTICE` when redistributing.
