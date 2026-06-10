// Package approval implements the blocking-approval protocol: request,
// decide, edit, cancel, expire. State machine in SQLite (atomic transitions
// with WHERE state='pending' guards). 5-min hard expiry per TODO §7.3.
// Phase 3.
package approval
