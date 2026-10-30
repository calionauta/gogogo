// SCOPE:plugin — Installer engine: the desktop-sync guidance shared by the CLI
// help and the MCP tool. One string, so the two transports cannot drift.
package installer

// SyncPrerequisites is the answer both `gogogo --help` and the MCP
// `sync_prerequisites` tool return. Keep it imperative and point at the
// normative doc instead of restating every detail here.
const SyncPrerequisites = `Desktop sync needs a NATS listener the device can reach, plus a token.

  1. Hub: set NATS_LEAFNODE_LISTEN=host:port (the leaf protocol has its own
     listener; NATS_LISTEN is the client port and cannot accept a leaf).
  2. Both sides: NATS_TOKEN=... (a non-loopback client or leaf listener is
     refused without one). The token is also the leaf credential.
  3. Endpoint: CLOUD_SYNC_URL=nats://<hub>:<leaf-port> so issued device
     tokens carry it; the desktop saves it in <DATA_DIR>/sync.json (0600).
  4. Identity: LICENSE_MODE=account activation adopts the server user id;
     a local-only account never joins the server's rows.
  5. Path: Tailscale/WireGuard is the lean route. Public exposure needs TLS
     per connection type and per-device credentials (NKey/JWT). Never point
     a build at gogogo.calionauta.com — that host is only the demo.
  6. Prove it: make desktop-sync-smoke (todo on the desktop, same row on the
     server under the same user id).

  Full recipe: docs/desktop-mobile.md#cloud-sync`
