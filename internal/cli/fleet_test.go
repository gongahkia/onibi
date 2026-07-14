package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFleetEndpointSelectsAdapter(t *testing.T) {
	out, _ := executeRoot(t, "fleet", "endpoint", "relay", "https://relay.example.test", "--json", "--color", "never")
	var got struct {
		Version  uint16 `json:"version"`
		Adapter  string `json:"adapter"`
		Endpoint struct {
			Kind string `json:"kind"`
			URL  string `json:"url"`
		} `json:"endpoint"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Adapter != "relay" || got.Endpoint.Kind != "relay" || got.Endpoint.URL != "https://relay.example.test" {
		t.Fatalf("fleet endpoint = %#v", got)
	}
}

func TestFleetEndpointRejectsInvalidAdapterInput(t *testing.T) {
	_, _, err := executeRootAllowError(t, "fleet", "endpoint", "ssh", "onibi@host.example.test:not-a-port", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "invalid ssh endpoint") {
		t.Fatalf("fleet endpoint error = %v", err)
	}
}
