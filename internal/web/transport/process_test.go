package transport

import "testing"

func TestMergedEnvOverridesExistingKey(t *testing.T) {
	t.Setenv("TUNNEL_TOKEN", "old")
	got := mergedEnv([]string{"TUNNEL_TOKEN=new"})
	var count int
	for _, entry := range got {
		if entry == "TUNNEL_TOKEN=old" {
			t.Fatalf("old token survived in env: %#v", got)
		}
		if entry == "TUNNEL_TOKEN=new" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("new token count = %d in %#v", count, got)
	}
}
