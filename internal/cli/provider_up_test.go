package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
)

func TestProviderOptionsFromEnvMatrix(t *testing.T) {
	t.Setenv("ONIBI_MATRIX_HOMESERVER", "https://matrix.example")
	t.Setenv("ONIBI_MATRIX_ACCESS_TOKEN", "tok")
	t.Setenv("ONIBI_MATRIX_ROOM_ID", "!room:example")
	t.Setenv("ONIBI_MATRIX_OWNER_USER_ID", "@owner:example")
	t.Setenv("ONIBI_MATRIX_OWNER_DEVICE_ID", "OWNER")
	t.Setenv("ONIBI_MATRIX_ALLOW_ENCRYPTED", "1")
	t.Setenv("ONIBI_MATRIX_SAS_VERIFIED", "1")
	opts, label, err := providerOptionsFromEnv("matrix")
	if err != nil {
		t.Fatal(err)
	}
	if label != "Matrix" || opts.Matrix.RoomID != "!room:example" || opts.Matrix.OwnerUserID != "@owner:example" || opts.Matrix.OwnerDeviceID != "OWNER" || !opts.Matrix.AllowEncrypted || !opts.Matrix.SASVerified {
		t.Fatalf("opts=%#v label=%q", opts, label)
	}
}

func TestRunEnvProviderUpRequiresExperimentalOptIn(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cfg := config.Default()
	cfg.Transport.Mode = "matrix"
	err := runEnvProviderUp(cmd, config.Paths{}, nil, cfg, nil, time.Time{}, "")
	if err == nil || err.Error() != "matrix is deferred in v1; set experimental.providers=true to opt into unsupported provider behavior" {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestProviderOptionsFromEnvSlackAllowlist(t *testing.T) {
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb")
	t.Setenv("ONIBI_SLACK_ALLOWED_CHANNELS", "C1,C2")
	t.Setenv("ONIBI_SLACK_ALLOWED_DM_USERS", "U1")
	opts, _, err := providerOptionsFromEnv("slack")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Slack.AllowedIDs) != 2 || opts.Slack.AllowedDMUsers[0] != "U1" {
		t.Fatalf("opts=%#v", opts.Slack)
	}
}

func TestProviderOptionsFromEnvRejectsMissing(t *testing.T) {
	if _, _, err := providerOptionsFromEnv("discord"); err == nil {
		t.Fatal("expected missing discord token error")
	}
	if !isEnvChatTransport("matrix") || !isEnvChatTransport("slack") || !isEnvChatTransport("discord") || !isEnvChatTransport("zulip") {
		t.Fatal("chat transport classification failed")
	}
}

func TestProviderOptionsFromEnvZulip(t *testing.T) {
	t.Setenv("ONIBI_ZULIP_URL", "https://zulip.example")
	t.Setenv("ONIBI_ZULIP_EMAIL", "onibi-bot@example.com")
	t.Setenv("ONIBI_ZULIP_API_KEY", "key")
	t.Setenv("ONIBI_ZULIP_STREAM", "onibi")
	t.Setenv("ONIBI_ZULIP_TOPIC_PREFIX", "sess-")
	t.Setenv("ONIBI_ZULIP_OWNER_EMAIL", "owner@example.com")
	opts, label, err := providerOptionsFromEnv("zulip")
	if err != nil {
		t.Fatal(err)
	}
	if label != "Zulip" || opts.Zulip.Stream != "onibi" || opts.Zulip.TopicPrefix != "sess-" || opts.Zulip.OwnerEmail != "owner@example.com" {
		t.Fatalf("opts=%#v label=%q", opts.Zulip, label)
	}
}
