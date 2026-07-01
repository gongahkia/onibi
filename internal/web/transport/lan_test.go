package transport

import "testing"

func TestWebPairURLsFormatsHostsAndSuppressesDuplicates(t *testing.T) {
	got := WebPairURLs("tok", 8443, []string{"192.168.1.20", "fd00::1", "192.168.1.20"}, "host.local")
	want := []string{
		"https://192.168.1.20:8443/pair/tok",
		"https://[fd00::1]:8443/pair/tok",
		"https://host.local:8443/pair/tok",
	}
	if len(got) != len(want) {
		t.Fatalf("urls = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urls = %#v", got)
		}
	}
}

func TestWebPairURLsFallsBackToLocalhost(t *testing.T) {
	got := WebPairURLs("tok", 9443, nil, "")
	if len(got) != 1 || got[0] != "https://localhost:9443/pair/tok" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestLANLoopbackResolved(t *testing.T) {
	pt, err := Resolve(t.Context(), ResolverOptions{Mode: "lan-loopback", Port: 18443})
	if err != nil {
		t.Fatal(err)
	}
	got := pt.URLs("tok")
	if pt.Mode != ModeLANLoopback || len(got) != 1 || got[0] != "https://127.0.0.1:18443/pair/tok" {
		t.Fatalf("mode=%q urls=%#v", pt.Mode, got)
	}
}
