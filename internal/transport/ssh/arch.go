package ssh

import (
	"fmt"
	"strings"
)

type Platform struct {
	GOOS   string
	GOARCH string
	GOARM  string
}

func (c *Client) DetectArch() (Platform, error) {
	session, err := c.NewSession()
	if err != nil {
		return Platform{}, err
	}
	defer session.Close()
	out, err := session.Output("uname -sm")
	if err != nil {
		return Platform{}, err
	}
	return ParseUnameSM(string(out))
}

func ParseUnameSM(out string) (Platform, error) {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return Platform{}, fmt.Errorf("ssh: malformed uname -sm output: %q", out)
	}
	key := fields[0] + " " + fields[1]
	switch key {
	case "Linux aarch64":
		return Platform{GOOS: "linux", GOARCH: "arm64"}, nil
	case "Linux armv7l":
		return Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"}, nil
	case "Linux armv6l":
		return Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"}, nil
	case "Linux x86_64":
		return Platform{GOOS: "linux", GOARCH: "amd64"}, nil
	case "Darwin arm64":
		return Platform{GOOS: "darwin", GOARCH: "arm64"}, nil
	case "Darwin x86_64":
		return Platform{GOOS: "darwin", GOARCH: "amd64"}, nil
	default:
		return Platform{}, fmt.Errorf("ssh: unsupported uname -sm output: %q", strings.TrimSpace(out))
	}
}

func (p Platform) ArtifactSuffix() string {
	if p.GOARCH == "arm" && p.GOARM != "" {
		return p.GOOS + "_armv" + p.GOARM
	}
	if p.GOOS == "darwin" && p.GOARCH == "amd64" {
		return "darwin_x86_64"
	}
	if p.GOOS == "linux" && p.GOARCH == "amd64" {
		return "linux_x86_64"
	}
	return p.GOOS + "_" + p.GOARCH
}
