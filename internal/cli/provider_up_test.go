package cli

import "testing"

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
	if !isEnvChatTransport("matrix") || !isEnvChatTransport("slack") || !isEnvChatTransport("discord") {
		t.Fatal("chat transport classification failed")
	}
	if !isNotifyTransport("pushover") || !isNotifyTransport("ntfy") || !isNotifyTransport("gotify") {
		t.Fatal("notify transport classification failed")
	}
}
