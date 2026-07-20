package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
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
	if !isEnvChatTransport("matrix") || !isEnvChatTransport("slack") || !isEnvChatTransport("discord") || !isEnvChatTransport("zulip") || !isEnvChatTransport("irc") || !isEnvChatTransport("signal") {
		t.Fatal("chat transport classification failed")
	}
	if !isNotifyTransport("pushover") || !isNotifyTransport("ntfy") {
		t.Fatal("notify transport classification failed")
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

func TestProviderOptionsFromEnvIRC(t *testing.T) {
	t.Setenv("ONIBI_IRC_ADDR", "irc.example:6697")
	t.Setenv("ONIBI_IRC_NICK", "onibi")
	t.Setenv("ONIBI_IRC_USERNAME", "onibi-account")
	t.Setenv("ONIBI_IRC_PASSWORD", "secret")
	t.Setenv("ONIBI_IRC_OWNER_NICK", "owner")
	t.Setenv("ONIBI_IRC_PLAINTEXT", "1")
	opts, label, err := providerOptionsFromEnv("irc")
	if err != nil {
		t.Fatal(err)
	}
	if label != "IRC" || opts.IRC.Addr != "irc.example:6697" || opts.IRC.Username != "onibi-account" || opts.IRC.OwnerNick != "owner" || !opts.IRC.Plaintext {
		t.Fatalf("opts=%#v label=%q", opts.IRC, label)
	}
}

func TestProviderOptionsFromEnvSignal(t *testing.T) {
	t.Setenv("ONIBI_SIGNAL_RPC_URL", "http://127.0.0.1:6001")
	t.Setenv("ONIBI_SIGNAL_ACCOUNT", "+15550001")
	t.Setenv("ONIBI_SIGNAL_RECIPIENTS", "+15550002,+15550003")
	t.Setenv("ONIBI_SIGNAL_OWNER", "+15550002")
	opts, label, err := providerOptionsFromEnv("signal")
	if err != nil {
		t.Fatal(err)
	}
	if label != "Signal" || opts.Signal.RPCURL != "http://127.0.0.1:6001" || opts.Signal.Account != "+15550001" || opts.Signal.Owner != "+15550002" {
		t.Fatalf("opts=%#v label=%q", opts.Signal, label)
	}
	if len(opts.Signal.Recipients) != 2 || opts.Signal.Recipients[1] != "+15550003" {
		t.Fatalf("recipients=%#v", opts.Signal.Recipients)
	}
}

func TestEnvProviderActionWebAddr(t *testing.T) {
	opts := envProviderOptions{Ntfy: daemon.NtfyOptions{}}
	if got := envProviderActionWebAddr("ntfy", opts, ":8443"); got != "" {
		t.Fatalf("ntfy addr = %q", got)
	}
}
