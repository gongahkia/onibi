// Package tmux is the optional --attach-tmux backend. Uses tmux send-keys -l
// for injection and tmux capture-pane for output. Phase 7. Known bracketed-
// paste edge case with extended-keys-format=csi-u — verify-and-retry on
// dispatch (see TODO §10 risks row).
package tmux
