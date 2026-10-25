package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gongahkia/onibi/internal/ghostty"
)

func TestCloseRequiresTheExactReviewedMembership(t *testing.T) {
	for _, kind := range []string{"close_window", "close_tab"} {
		for _, change := range []string{"added", "removed", "moved", "renamed", "reordered"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				s, backend := testServer(t, testOrigin)
				b := pairBrowser(t, s)
				backend.targets = []ghostty.Target{{ID: "a", WindowID: "w", TabID: "t"}, {ID: "b", WindowID: "w", TabID: "t"}}
				a := ghostty.Action{Kind: kind, WindowID: "w", ExpectedTargets: []string{"a", "b"}}
				if kind == "close_tab" {
					a.TabID = "t"
				}
				switch change {
				case "added":
					backend.targets = append(backend.targets, ghostty.Target{ID: "c", WindowID: "w", TabID: "t"})
				case "removed":
					backend.targets = backend.targets[:1]
				case "moved":
					backend.targets[1].WindowID = "another-window"
				case "renamed":
					backend.targets[1].Title = "New label"
				case "reordered":
					backend.targets[0], backend.targets[1] = backend.targets[1], backend.targets[0]
				}
				raw, _ := json.Marshal(actionRequest{ID: "close-reviewed-membership", Action: a})
				r := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw))
				allowed := change == "renamed" || change == "reordered"
				if allowed && (r.State != "completed" || len(backend.calls) != 1) || !allowed && (r.State != "rejected" || len(backend.calls) != 0) {
					t.Fatalf("allowed=%v receipt=%+v calls=%d", allowed, r, len(backend.calls))
				}
			})
		}
	}
}

func TestCloseScopeShapeCannotBeOmittedOrSmuggled(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	for i, a := range []ghostty.Action{
		{Kind: "close_window", WindowID: "window-1"},
		{Kind: "close_tab", WindowID: "window-1", TabID: "tab-1", ExpectedTargets: []string{"surface-1", "surface-1"}},
		{Kind: "close_pane", TargetID: "surface-1", ExpectedTargets: []string{"other"}},
		{Kind: "input", TargetID: "surface-1", Text: "no", ExpectedTargets: []string{"surface-1"}},
	} {
		raw, _ := json.Marshal(actionRequest{ID: fmt.Sprintf("close-invalid-scope-%d", i), Action: a})
		if got := request(s, b, http.MethodPost, "/api/actions", raw); got.Code != http.StatusBadRequest {
			t.Fatalf("accepted invalid scope: %d %s", got.Code, got.Body)
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid close reached backend")
	}
}
