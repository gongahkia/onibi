package daemon

import "testing"

func TestExperimentalProvidersAreDisabledByDefault(t *testing.T) {
	if New(Options{}).ExperimentalProviders {
		t.Fatal("providers enabled by default")
	}
	if !New(Options{ExperimentalProviders: true}).ExperimentalProviders {
		t.Fatal("explicit provider opt-in not retained")
	}
}
