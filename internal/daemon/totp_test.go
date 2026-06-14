package daemon

import (
	"context"
	"testing"
	"time"
)

func TestTOTPGraceWithinWindow(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	base := time.Unix(1000, 0)
	withTOTPNow(t, base)
	d.recordTOTPSuccess(ctx, 100)
	withTOTPNow(t, base.Add(30*time.Second))
	if !d.withinTOTPGrace(ctx, 100) {
		t.Fatal("expected TOTP grace")
	}
}

func TestTOTPGraceExpired(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	base := time.Unix(1000, 0)
	withTOTPNow(t, base)
	d.recordTOTPSuccess(ctx, 100)
	withTOTPNow(t, base.Add(70*time.Second))
	if d.withinTOTPGrace(ctx, 100) {
		t.Fatal("expected expired TOTP grace")
	}
}

func TestTOTPGraceIsPerChat(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	base := time.Unix(1000, 0)
	withTOTPNow(t, base)
	d.recordTOTPSuccess(ctx, 100)
	withTOTPNow(t, base.Add(30*time.Second))
	if d.withinTOTPGrace(ctx, 200) {
		t.Fatal("unexpected TOTP grace for another chat")
	}
}

func withTOTPNow(t *testing.T, now time.Time) {
	t.Helper()
	old := totpNow
	totpNow = func() time.Time { return now }
	t.Cleanup(func() { totpNow = old })
}
