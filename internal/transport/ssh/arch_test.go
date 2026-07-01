package ssh

import "testing"

func TestParseUnameSM(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Platform
	}{
		{name: "linux arm64", in: "Linux aarch64\n", want: Platform{GOOS: "linux", GOARCH: "arm64"}},
		{name: "linux armv7", in: "Linux armv7l\n", want: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"}},
		{name: "linux armv6", in: "Linux armv6l\n", want: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"}},
		{name: "linux amd64", in: "Linux x86_64\n", want: Platform{GOOS: "linux", GOARCH: "amd64"}},
		{name: "darwin arm64", in: "Darwin arm64\n", want: Platform{GOOS: "darwin", GOARCH: "arm64"}},
		{name: "darwin amd64", in: "Darwin x86_64\n", want: Platform{GOOS: "darwin", GOARCH: "amd64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUnameSM(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("platform = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseUnameSMRejectsUnsupported(t *testing.T) {
	if _, err := ParseUnameSM("FreeBSD amd64\n"); err == nil {
		t.Fatal("expected unsupported error")
	}
	if _, err := ParseUnameSM("Linux\n"); err == nil {
		t.Fatal("expected malformed error")
	}
}

func TestPlatformArtifactSuffix(t *testing.T) {
	tests := []struct {
		platform Platform
		want     string
	}{
		{platform: Platform{GOOS: "linux", GOARCH: "arm64"}, want: "linux_arm64"},
		{platform: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"}, want: "linux_armv7"},
		{platform: Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"}, want: "linux_armv6"},
		{platform: Platform{GOOS: "linux", GOARCH: "amd64"}, want: "linux_x86_64"},
		{platform: Platform{GOOS: "darwin", GOARCH: "arm64"}, want: "darwin_arm64"},
		{platform: Platform{GOOS: "darwin", GOARCH: "amd64"}, want: "darwin_x86_64"},
	}
	for _, tt := range tests {
		if got := tt.platform.ArtifactSuffix(); got != tt.want {
			t.Fatalf("suffix = %q, want %q", got, tt.want)
		}
	}
}
