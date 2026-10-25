package lsp_test

import (
	"context"
	"encoding/json"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/testutil"
	"os"
	"testing"
	"time"
)

func TestApplyFixtureProcess(t *testing.T) {
	if os.Getenv("SHACK_APPLY_FIXTURE") != "1" {
		return
	}
	testutil.ActionServer()
	os.Exit(0)
}

func TestApplyReviewDoesNotBlockProtocolAndPreservesResult(t *testing.T) {
	for _, applied := range []bool{true, false} {
		t.Run(map[bool]string{true: "accept", false: "reject"}[applied], func(t *testing.T) {
			t.Setenv("SHACK_APPLY_FIXTURE", "1")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered, review := make(chan struct{}), make(chan struct{})
			c, err := lsp.Start(ctx, []string{os.Args[0], "-test.run=^TestApplyFixtureProcess$"}, t.TempDir(), nil, func(ctx context.Context, p lsp.ApplyEditParams) lsp.ApplyEditResult {
				close(entered)
				select {
				case <-review:
					return lsp.ApplyEditResult{Applied: applied, FailureReason: "review decision"}
				case <-ctx.Done():
					return lsp.ApplyEditResult{FailureReason: ctx.Err().Error()}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			result := make(chan json.RawMessage, 1)
			go func() {
				raw, _ := c.Request(ctx, "workspace/executeCommand", map[string]any{"command": "fixture.apply", "arguments": []any{map[string]any{"label": "fix", "edit": map[string]any{"changes": map[string]any{}}}}})
				result <- raw
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("review never started")
			}
			raw, err := c.Request(ctx, "fixture.ping", nil)
			if err != nil || string(raw) != `"pong"` {
				t.Fatal("review blocked reader", string(raw), err)
			}
			close(review)
			select {
			case raw = <-result:
			case <-ctx.Done():
				t.Fatal("no apply result")
			}
			var response lsp.ApplyEditResult
			if json.Unmarshal(raw, &response) != nil || response.Applied != applied || response.FailureReason != "review decision" {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestApplyCancellationAndTransportClose(t *testing.T) {
	for _, method := range []string{"fixture.cancel", "fixture.apply"} {
		t.Run(method, func(t *testing.T) {
			t.Setenv("SHACK_APPLY_FIXTURE", "1")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			entered, stopped := make(chan struct{}), make(chan struct{})
			c, err := lsp.Start(ctx, []string{os.Args[0], "-test.run=^TestApplyFixtureProcess$"}, t.TempDir(), nil, func(ctx context.Context, p lsp.ApplyEditParams) lsp.ApplyEditResult {
				close(entered)
				<-ctx.Done()
				close(stopped)
				return lsp.ApplyEditResult{FailureReason: ctx.Err().Error()}
			})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			go func() {
				c.Request(ctx, "workspace/executeCommand", map[string]any{"command": method, "arguments": []any{map[string]any{"edit": map[string]any{}}}})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("no review")
			}
			if method == "fixture.apply" {
				c.Close()
			}
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("handler did not cancel")
			}
		})
	}
}

func TestTimedOutCommandSendsCancellation(t *testing.T) {
	t.Setenv("SHACK_APPLY_FIXTURE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	notices := make(chan lsp.Notification, 4)
	c, err := lsp.Start(ctx, []string{os.Args[0], "-test.run=^TestApplyFixtureProcess$"}, t.TempDir(), func(n lsp.Notification) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	request, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, err = c.Request(request, "workspace/executeCommand", map[string]any{"command": "fixture.hang"}); err == nil {
		t.Fatal("no timeout")
	}
	select {
	case n := <-notices:
		if n.Method != "fixture.cancelled" {
			t.Fatal(n.Method)
		}
	case <-ctx.Done():
		t.Fatal("no cancellation")
	}
	if !c.Alive() {
		t.Fatal("server killed for an ordinary response timeout")
	}
}
