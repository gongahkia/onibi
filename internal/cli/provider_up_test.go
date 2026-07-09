package cli

import (
	"testing"

	"github.com/gongahkia/onibi/internal/daemon"
)

func TestProviderOptionsFromEnvMatrix(t *testing.T) {
	t.Setenv("ONIBI_MATRIX_HOMESERVER", "https://matrix.example")
	t.Setenv("ONIBI_MATRIX_ACCESS_TOKEN", "tok")
	t.Setenv("ONIBI_MATRIX_ROOM_ID", "!room:example")
	t.Setenv("ONIBI_MATRIX_OWNER_USER_ID", "@owner:example")
	t.Setenv("ONIBI_MATRIX_ALLOW_ENCRYPTED", "1")
	opts, label, err := providerOptionsFromEnv("matrix")
	if err != nil {
		t.Fatal(err)
	}
	if label != "Matrix" || opts.Matrix.RoomID != "!room:example" || opts.Matrix.OwnerUserID != "@owner:example" || !opts.Matrix.AllowEncrypted {
		t.Fatalf("opts=%#v label=%q", opts, label)
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
	if !isEnvChatTransport("matrix") || !isEnvChatTransport("slack") || !isEnvChatTransport("discord") || !isEnvChatTransport("zulip") || !isEnvChatTransport("irc") {
		t.Fatal("chat transport classification failed")
	}
	if !isNotifyTransport("pushover") || !isNotifyTransport("ntfy") || !isNotifyTransport("gotify") || !isNotifyTransport("apns") || !isNotifyTransport("sms") || !isNotifyTransport("email") {
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

func TestProviderOptionsFromEnvAPNs(t *testing.T) {
	t.Setenv("ONIBI_APNS_KEY_PATH", "/tmp/AuthKey_ABC123DEFG.p8")
	t.Setenv("ONIBI_APNS_KEY_ID", "ABC123DEFG")
	t.Setenv("ONIBI_APNS_TEAM_ID", "TEAM123456")
	t.Setenv("ONIBI_APNS_TOPIC", "com.example.onibi")
	t.Setenv("ONIBI_APNS_DEVICE_TOKEN", "abc123")
	t.Setenv("ONIBI_APNS_ENV", "development")
	opts, label, err := providerOptionsFromEnv("apns")
	if err != nil {
		t.Fatal(err)
	}
	if label != "APNs" || opts.APNs.Topic != "com.example.onibi" || opts.APNs.Environment != "development" || opts.APNs.DeviceToken != "abc123" {
		t.Fatalf("opts=%#v label=%q", opts.APNs, label)
	}
}

func TestProviderOptionsFromEnvSMS(t *testing.T) {
	t.Setenv("ONIBI_TWILIO_ACCOUNT_SID", "AC123")
	t.Setenv("ONIBI_TWILIO_AUTH_TOKEN", "tok")
	t.Setenv("ONIBI_TWILIO_MESSAGING_SERVICE_SID", "MG123")
	t.Setenv("ONIBI_SMS_TO", "+15550002")
	t.Setenv("ONIBI_SMS_ACTION_BASE_URL", "https://onibi.example")
	opts, label, err := providerOptionsFromEnv("sms")
	if err != nil {
		t.Fatal(err)
	}
	if label != "SMS" || opts.SMS.MessagingServiceSID != "MG123" || opts.SMS.To != "+15550002" || opts.SMS.ActionBaseURL != "https://onibi.example" {
		t.Fatalf("opts=%#v label=%q", opts.SMS, label)
	}
}

func TestProviderOptionsFromEnvEmail(t *testing.T) {
	t.Setenv("ONIBI_SMTP_ADDR", "smtp.example:587")
	t.Setenv("ONIBI_SMTP_HOST", "smtp.example")
	t.Setenv("ONIBI_SMTP_USERNAME", "user")
	t.Setenv("ONIBI_SMTP_PASSWORD", "pass")
	t.Setenv("ONIBI_EMAIL_FROM", "onibi@example.com")
	t.Setenv("ONIBI_EMAIL_TO", "owner@example.com")
	t.Setenv("ONIBI_EMAIL_ACTION_BASE_URL", "https://onibi.example")
	opts, label, err := providerOptionsFromEnv("email")
	if err != nil {
		t.Fatal(err)
	}
	if label != "Email" || opts.Email.Addr != "smtp.example:587" || opts.Email.Host != "smtp.example" || opts.Email.To != "owner@example.com" || opts.Email.ActionBaseURL != "https://onibi.example" {
		t.Fatalf("opts=%#v label=%q", opts.Email, label)
	}
}

func TestEnvProviderActionWebAddr(t *testing.T) {
	opts := envProviderOptions{SMS: daemon.SMSOptions{ActionBaseURL: "https://onibi.example"}}
	if got := envProviderActionWebAddr("sms", opts, ":8443"); got != ":8443" {
		t.Fatalf("sms addr = %q", got)
	}
	opts = envProviderOptions{Ntfy: daemon.NtfyOptions{}}
	if got := envProviderActionWebAddr("ntfy", opts, ":8443"); got != "" {
		t.Fatalf("ntfy addr = %q", got)
	}
}
