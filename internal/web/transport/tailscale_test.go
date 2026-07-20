package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTailscalePrivateDetectRequiresRunningHTTPSAndDNSName(t *testing.T) {
	ts := testTailscalePrivate(statusRunningPrivate(), servePrivate())
	ok, err := ts.Detect(context.Background())
	if err != nil || !ok {
		t.Fatalf("Detect ok=%v err=%v", ok, err)
	}
	for _, status := range []string{
		`{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{}}}`,
		`{"BackendState":"Running","Self":{"DNSName":"bad host","CapMap":{"https":{}}}}`,
		`{"BackendState":"NeedsLogin","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{}}}}`,
	} {
		if _, err := testTailscalePrivate(status, servePrivate()).Detect(context.Background()); err == nil {
			t.Fatalf("Detect(%s) unexpectedly succeeded", status)
		}
	}
}

func TestTailscalePrivateEnableUsesServeAndPollsURL(t *testing.T) {
	r := &fakeTSRunner{outputs: map[string][]byte{
		"tailscale status --json":                              []byte(statusRunningPrivate()),
		"tailscale serve --bg https+insecure://localhost:8443": []byte("ok\n"),
		"tailscale serve status --json":                        []byte(servePrivate()),
	}}
	ts := &TailscalePrivate{Bin: "tailscale", runner: r}
	if err := ts.Enable(context.Background(), 8443); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tailscale", "status", "--json"},
		{"tailscale", "serve", "--bg", "https+insecure://localhost:8443"},
		{"tailscale", "status", "--json"},
		{"tailscale", "serve", "status", "--json"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestTailscalePrivateURLParsesActiveServeStatus(t *testing.T) {
	got, err := testTailscalePrivate(statusRunningPrivate(), servePrivate()).URL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://dev.tail.ts.net/" {
		t.Fatalf("url = %q", got)
	}
}

func TestTailscalePrivateURLRejectsInactiveOrInvalidServeStatus(t *testing.T) {
	for _, body := range []string{"{}", "not-json"} {
		_, err := testTailscalePrivate(statusRunningPrivate(), body).URL(context.Background())
		if err == nil {
			t.Fatalf("URL(%s) unexpectedly succeeded", body)
		}
	}
}

func TestExecCommandRunnerIgnoresStderrWarning(t *testing.T) {
	out, err := (execCommandRunner{}).Run(context.Background(), "/bin/sh", "-c", "echo warning >&2; cat <<'JSON'\n"+statusRunningPrivate()+"\nJSON")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("stdout is not clean JSON: %q", out)
	}
}

func TestTailscalePrivateDisableTurnsServeOff(t *testing.T) {
	r := &fakeTSRunner{outputs: map[string][]byte{
		"tailscale serve --bg off": []byte("off\n"),
	}}
	ts := &TailscalePrivate{Bin: "tailscale", runner: r}
	if err := ts.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"tailscale", "serve", "--bg", "off"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v", r.calls)
	}
}

func TestTailscalePrivateDisableReportsCleanupFailure(t *testing.T) {
	r := &fakeTSRunner{errs: map[string]error{
		"tailscale serve --bg off": errors.New("boom"),
	}}
	err := (&TailscalePrivate{Bin: "tailscale", runner: r}).Disable(context.Background())
	var diag *DiagnosticError
	if !errors.As(err, &diag) || diag.Code != DiagCleanup {
		t.Fatalf("err = %#v", err)
	}
}

func statusRunningPrivate() string {
	return `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{}}}}`
}

func servePrivate() string {
	return `{"Web":{"dev.tail.ts.net:443":{"Handlers":{"/":{"Proxy":"https+insecure://localhost:8443"}}}}}`
}

func testTailscalePrivate(status, serve string) *TailscalePrivate {
	return &TailscalePrivate{Bin: "tailscale", runner: &fakeTSRunner{outputs: map[string][]byte{
		"tailscale status --json":       []byte(status),
		"tailscale serve status --json": []byte(serve),
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
