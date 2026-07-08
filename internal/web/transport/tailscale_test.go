package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTailscaleDetectRequiresRunningFunnelCaps(t *testing.T) {
	ts := testTailscale(statusRunningFunnel(), serveActive())
	ok, err := ts.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("detect returned false")
	}
}

func TestTailscaleDetectRejectsMissingFunnelPort(t *testing.T) {
	ts := testTailscale(`{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{},"funnel":{}}}}`, serveActive())
	ok, err := ts.Detect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "public port 443") {
		t.Fatalf("Detect err = %v", err)
	}
	if ok {
		t.Fatal("detect returned true")
	}
}

func TestTailscaleDetectRejectsLoggedOutBackend(t *testing.T) {
	ts := testTailscale(`{"BackendState":"NeedsLogin","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{}}}`, serveActive())
	ok, err := ts.Detect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NeedsLogin") {
		t.Fatalf("Detect err = %v", err)
	}
	if ok {
		t.Fatal("detect returned true")
	}
}

func TestTailscaleDetectRejectsMissingHTTPSAndFunnelCaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{
			name:   "https",
			status: `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"funnel":{},"https://tailscale.com/cap/funnel-ports?ports=443":{}}}}`,
			want:   "HTTPS",
		},
		{
			name:   "funnel",
			status: `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{},"https://tailscale.com/cap/funnel-ports?ports=443":{}}}}`,
			want:   "Funnel",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testTailscale(tc.status, serveActive()).Detect(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Detect err = %v", err)
			}
		})
	}
}

func TestTailscaleEnableUsesHTTPSInsecureTargetAndPollsURL(t *testing.T) {
	r := &fakeTSRunner{outputs: map[string][]byte{
		"tailscale status --json":                               []byte(statusRunningFunnel()),
		"tailscale funnel --bg https+insecure://localhost:8443": []byte("ok\n"),
		"tailscale funnel status --json":                        []byte(serveActive()),
	}}
	ts := &Tailscale{Bin: "tailscale", runner: r}
	if err := ts.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tailscale", "status", "--json"},
		{"tailscale", "funnel", "--bg", "https+insecure://localhost:8443"},
		{"tailscale", "funnel", "status", "--json"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestTailscaleURLParsesFunnelStatus(t *testing.T) {
	got, err := testTailscale(statusRunningFunnel(), serveActive()).URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://dev.tail.ts.net/" {
		t.Fatalf("url = %q", got)
	}
}

func TestTailscaleURLFallsBackToServeStatus(t *testing.T) {
	r := &fakeTSRunner{
		outputs: map[string][]byte{
			"tailscale status --json":       []byte(statusRunningFunnel()),
			"tailscale serve status --json": []byte(serveActive()),
		},
		errs: map[string]error{
			"tailscale funnel status --json": errors.New("unknown command"),
		},
	}
	ts := &Tailscale{Bin: "tailscale", runner: r}
	got, err := ts.URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://dev.tail.ts.net/" {
		t.Fatalf("url = %q", got)
	}
	want := [][]string{
		{"tailscale", "funnel", "status", "--json"},
		{"tailscale", "serve", "status", "--json"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestTailscaleURLParsesServeStatusBody(t *testing.T) {
	got, err := funnelURLFromServeStatus([]byte(`{
		"AllowFunnel": {
			"dev.tail.ts.net:8443": true,
			"dev.tail.ts.net:443": true
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://dev.tail.ts.net/" {
		t.Fatalf("url = %q", got)
	}
}

func TestTailscaleURLRejectsInvalidJSON(t *testing.T) {
	_, err := funnelURLFromServeStatus([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected parse error")
	}
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagURLParse {
		t.Fatalf("err = %#v", err)
	}
}

func TestExecCommandRunnerIgnoresStderrWarning(t *testing.T) {
	out, err := (execCommandRunner{}).Run(context.Background(), "/bin/sh", "-c", "echo warning >&2; cat <<'JSON'\n"+statusRunningFunnel()+"\nJSON")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("stdout is not clean JSON: %q", out)
	}
}

func TestTailscaleDisableTurnsFunnelOff(t *testing.T) {
	r := &fakeTSRunner{outputs: map[string][]byte{
		"tailscale funnel --bg off": []byte("off\n"),
	}}
	ts := &Tailscale{Bin: "tailscale", runner: r}
	if err := ts.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tailscale", "funnel", "--bg", "off"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestTailscaleDisableReportsCleanupFailure(t *testing.T) {
	r := &fakeTSRunner{errs: map[string]error{
		"tailscale funnel --bg off": errors.New("boom"),
	}}
	ts := &Tailscale{Bin: "tailscale", runner: r}
	err := ts.Disable(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagCleanup {
		t.Fatalf("err = %#v", err)
	}
}

func statusRunningFunnel() string {
	return `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{},"funnel":{},"https://tailscale.com/cap/funnel-ports?ports=443,8443-8444":{}}}}`
}

func serveActive() string {
	return `{"AllowFunnel":{"dev.tail.ts.net:443":true},"Web":{"dev.tail.ts.net:443":{}}}`
}

func testTailscale(status, serve string) *Tailscale {
	return &Tailscale{Bin: "tailscale", runner: &fakeTSRunner{outputs: map[string][]byte{
		"tailscale status --json":        []byte(status),
		"tailscale funnel status --json": []byte(serve),
	}}}
}

type fakeTSRunner struct {
	outputs map[string][]byte
	errs    map[string]error
	calls   [][]string
}

func (r *fakeTSRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	key := strings.Join(call, " ")
	if err := r.errs[key]; err != nil {
		return nil, err
	}
	out, ok := r.outputs[key]
	if !ok {
		return nil, errors.New("unexpected command: " + key)
	}
	return out, nil
}
