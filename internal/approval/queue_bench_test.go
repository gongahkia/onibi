package approval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func BenchmarkApprovalDecisionRoundTrip(b *testing.B) {
	db, err := store.Open(filepath.Join(b.TempDir(), "approval-bench.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	q := New(db, DefaultTTL)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, ch, err := q.Request(ctx, "bench", "claude", "Bash", `{"command":"true"}`)
		if err != nil {
			b.Fatal(err)
		}
		if err := q.Decide(ctx, id, VerdictApprove, "", "", 1); err != nil {
			b.Fatal(err)
		}
		select {
		case d := <-ch:
			if d.Verdict != VerdictApprove {
				b.Fatalf("verdict = %s", d.Verdict)
			}
		default:
			b.Fatal("decision not delivered")
		}
	}
}
