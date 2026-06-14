package daemon

import (
	"context"
	"testing"
)

func TestStaleDefaultTargetClearedOnSessionEnd(t *testing.T) {
	ctx := context.Background()
	d := newApprovalDaemon(t)
	ended := NewSession("ended123", "ended", "claude", nil, 1024)
	live := NewSession("live123", "live", "claude", nil, 1024)
	if err := d.Registry.Add(ended); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Add(live); err != nil {
		t.Fatal(err)
	}
	d.setDefaultTarget(ctx, 100, ended.ID)
	d.setDefaultTarget(ctx, 200, live.ID)
	d.markSessionEnded(ctx, ended)
	if got := d.defaultTarget(ctx, 100); got != "" {
		t.Fatalf("stale default target = %q", got)
	}
	if got := d.defaultTarget(ctx, 200); got != live.ID {
		t.Fatalf("live default target = %q, want %q", got, live.ID)
	}
}
