package web

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

func TestCapabilityMapCoversTheActionContract(t *testing.T) {
	ids := map[string]Feature{}
	for _, f := range featureCatalog() {
		if _, ok := ids[f.ID]; ok {
			t.Fatalf("duplicate %s", f.ID)
		}
		ids[f.ID] = f
		if f.Implemented && !f.Automated {
			t.Fatalf("missing coverage decision for %s", f.ID)
		}
	}
	// Read the validator's cases so adding an action without a capability entry
	// fails this contract check rather than leaving a second stale allowlist.
	file, err := parser.ParseFile(token.NewFileSet(), "../ghostty/controls.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		sw, ok := node.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		sel, ok := sw.Tag.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Kind" {
			return true
		}
		for _, stmt := range sw.Body.List {
			for _, item := range stmt.(*ast.CaseClause).List {
				literal, ok := item.(*ast.BasicLit)
				if !ok {
					continue
				}
				id, _ := strconv.Unquote(literal.Value)
				if id == "control" {
					continue
				}
				count++
				if !ids[id].Implemented {
					t.Errorf("action %s missing from capability map", id)
				}
			}
		}
		return true
	})
	if count < 10 {
		t.Fatal("did not inspect action contract")
	}
	for _, control := range ghostty.Controls() {
		f, ok := ids["control:"+control.ID]
		if !ok || !f.RequiresFocus {
			t.Fatalf("control missing or wrong focus policy: %s", control.ID)
		}
	}
	for _, id := range []string{"agent_state", "mouse", "terminal_stream", "background_capture", "configuration"} {
		if ids[id].Implemented || ids[id].Automated {
			t.Fatalf("unsupported feature overstated: %s", id)
		}
	}
}
func featureByID(t *testing.T, r FeatureReport, id string) Feature {
	t.Helper()
	for _, f := range r.Features {
		if f.ID == id {
			return f
		}
	}
	t.Fatal("missing feature", id)
	return Feature{}
}
func TestCapabilitiesArePassiveAndDoNotPromoteSimulations(t *testing.T) {
	s, b := testServer(t, testOrigin)
	if got := request(s, browser{}, "GET", "/api/capabilities", nil); got.Code != 401 {
		t.Fatal("unauthenticated capability report")
	}
	browser := pairBrowser(t, s)
	s.recordInventory(time.Now(), nil)
	s.recordCapture(time.Now(), "pane", nil)
	s.recordFeature(ghostty.Action{Kind: "input"}, ghostty.Result{}, nil)
	got := request(s, browser, "GET", "/api/capabilities", nil)
	if got.Code != 200 || b.listCalls != 0 || len(b.calls) != 0 {
		t.Fatal("capability read invoked backend")
	}
	for _, f := range s.featureReport().Features {
		if f.Observation != nil || f.NativeVerification == "observed" {
			t.Fatal("simulated success promoted to native evidence")
		}
	}
	// A zero-value client supplies only its cached version. No native calls are
	// made: all observations here are injected to test reporting semantics.
	s.backend = &ghostty.Client{}
	report := s.featureReport()
	if featureByID(t, report, "inventory").NativeVerification != "observed" {
		t.Fatal("inventory observation lost")
	}
	input := featureByID(t, report, "input")
	if input.NativeVerification != "not_verified" || input.Observation.Outcome != "acknowledged" {
		t.Fatal("acknowledgement overstated as verified effect")
	}
	s.recordFeature(ghostty.Action{Kind: "close_tab"}, ghostty.Result{CloseState: "removed"}, nil)
	if featureByID(t, s.featureReport(), "close_tab").NativeVerification != "scope_removal_observed" {
		t.Fatal("scope observation lost")
	}
	s.recordFeature(ghostty.Action{Kind: "close_tab"}, ghostty.Result{}, errors.New("timeout"))
	if f := featureByID(t, s.featureReport(), "close_tab"); f.NativeVerification != "not_verified" || f.Observation.Outcome != "uncertain" {
		t.Fatal("failure retained verified effect")
	}
	s.recordCapture(time.Now().Add(time.Second), "pane", ghostty.ErrCapturePermission)
	if featureByID(t, s.featureReport(), "snapshot").NativeVerification != "not_verified" {
		t.Fatal("failed capture retained ready status")
	}
}
