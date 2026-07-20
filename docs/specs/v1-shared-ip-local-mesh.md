# V1 shared-IP local mesh

V1 local mesh interoperates over an existing IP link on macOS, Linux, native Windows, and Linux
under WSL. Supported profiles are `lan` and `wifi_hotspot`; a hotspot must already exist and both
devices must already be attached. The implementation neither creates a hotspot nor controls Wi-Fi
Direct or Bluetooth hardware.

`yeokcham local-mesh init` creates a versioned mesh configuration, creates or loads the local
Ed25519 identity, and stores a self-signed QUIC private key in the OS keystore. Its public
certificate is state-directory data. The listen endpoint must be a specific reachable IP address
and a nonzero UDP port; wildcard and IPv6 link-local addresses are rejected.

Peers exchange an Ed25519 public key and certificate SHA-256 fingerprint out of band. `local-mesh
peer add` stores both in the configuration. mDNS advertises an endpoint, identity, and selected
link type only. Discovery is untrusted: a connection requires the configured exact certificate
pin and the existing direct-peer proof bound to the QUIC TLS exporter. Unknown identities, pin
changes, malformed advertisements, and profile mismatches fail closed.

Run `yeokcham daemon serve --config <path>` on the receiving device. Run `yeokcham local-mesh
connect --config <path> --identity <peer-key>` on the initiating device; it advertises locally,
browses for the explicitly trusted peer, performs the authenticated connection, reports its remote
endpoint, then closes the connectivity check. A connect invocation requires its configured listen
endpoint to be free.

This V1 establishes authenticated interoperable connectivity only. It does not route stored
messages over the connection. Apple peer-to-peer, Linux/Windows Wi-Fi Direct, Bluetooth, native
hotspot provisioning, automatic fallback, and cross-OS native-P2P interoperability remain out of
scope.
