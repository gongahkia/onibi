// Package daemon owns the long-running lifecycle: signal handling, session
// registry, event bus, debounce. Phase 2 implements the registry; phase 3
// wires in the approval queue; phase 6 adds reply-to-message routing.
package daemon
