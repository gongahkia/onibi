// Package pty hosts coding agents under pseudo-terminals via creack/pty.
// Spawn owns the only master read loop and fans output through Hub. Consumers
// attach with Host.Subscribe; each subscriber is bounded, slow readers drop old
// frames, and drop/resize control frames let web clients recover without
// blocking agent output.
package pty
