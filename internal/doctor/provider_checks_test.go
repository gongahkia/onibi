package doctor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/slack"
)

func TestDoctorSlackProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "slack")
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("ONIBI_SLACK_APPROVAL_CHANNEL", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth.test"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "team_id": "T1", "bot_id": "B1"})
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "url": "wss://socket.example"})
		case strings.HasSuffix(r.URL.Path, "/conversations.info"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "channel": map[string]any{"id": "C1", "is_member": true}})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withSlackFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "Slack live API ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorSlackWarnsWhenNotMember(t *testing.T) {
	paths := doctorTestPaths(t, "slack")
	t.Setenv("ONIBI_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("ONIBI_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("ONIBI_SLACK_APPROVAL_CHANNEL", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth.test"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "team_id": "T1"})
		case strings.HasSuffix(r.URL.Path, "/apps.connections.open"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "url": "wss://socket.example"})
		case strings.HasSuffix(r.URL.Path, "/conversations.info"):
			writeDoctorJSON(t, w, map[string]any{"ok": true, "channel": map[string]any{"id": "C1", "is_member": false}})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withSlackFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "not a member") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorMatrixProviderFakeAPIEncryptedWarn(t *testing.T) {
	paths := doctorTestPaths(t, "matrix")
	t.Setenv("ONIBI_MATRIX_ACCESS_TOKEN", "matrix-token")
	t.Setenv("ONIBI_MATRIX_ROOM_ID", "!room:example")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeDoctorJSON(t, w, map[string]any{"user_id": "@bot:example"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeDoctorJSON(t, w, map[string]any{"users": map[string]any{"@bot:example": 100}})
		case strings.HasSuffix(r.URL.Path, "/joined_rooms"):
			writeDoctorJSON(t, w, map[string]any{"joined_rooms": []string{"!room:example"}})
		case strings.Contains(r.URL.Path, "/state/m.room.encryption"):
			writeDoctorJSON(t, w, map[string]any{"algorithm": "m.megolm.v1.aes-sha2"})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ONIBI_MATRIX_HOMESERVER", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "encrypted") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorDiscordProviderFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "discord")
	t.Setenv("ONIBI_DISCORD_TOKEN", "discord-token")
	t.Setenv("ONIBI_DISCORD_CHANNEL_ID", "C1")
	t.Setenv("ONIBI_DISCORD_APPLICATION_ID", "app1")
	t.Setenv("ONIBI_DISCORD_GUILD_ID", "G1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			writeDoctorJSON(t, w, discord.Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			writeDoctorJSON(t, w, discord.Channel{ID: "C1", GuildID: "G1"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/applications/app1/guilds/G1/commands"):
			writeDoctorJSON(t, w, []discord.ApplicationCommand{{ID: "cmd1", Name: "onibi"}})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withDiscordFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "slash command ok") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorDiscordWarnsOnSendPermission(t *testing.T) {
	paths := doctorTestPaths(t, "discord")
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_DISCORD_TOKEN", "discord-token")
	t.Setenv("ONIBI_DISCORD_CHANNEL_ID", "C1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth2/applications/@me"):
			writeDoctorJSON(t, w, discord.Application{ID: "app1", Name: "onibi"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/channels/C1"):
			writeDoctorJSON(t, w, discord.Channel{ID: "C1", GuildID: "G1"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/channels/C1/messages"):
			w.WriteHeader(http.StatusForbidden)
			writeDoctorJSON(t, w, map[string]any{"code": 50013, "message": "Missing Permissions"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withDiscordFactory(t, srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, "send permission failed") || !strings.Contains(check.Detail, "50013") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorNtfyProviderPublishSubscribeFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "ntfy")
	topic := "AbcdefGhij1234567890_Z"
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_NTFY_TOPIC", topic)
	published := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ws") {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			body := <-published
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"message":`+strconvQuote(body)+`}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		published <- string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("ONIBI_NTFY_BASE_URL", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "publish + WebSocket") {
		t.Fatalf("check = %#v", check)
	}
}

func TestDoctorGotifyProviderSendWSFakeAPI(t *testing.T) {
	paths := doctorTestPaths(t, "gotify")
	t.Setenv("ONIBI_DOCTOR_LIVE", "1")
	t.Setenv("ONIBI_GOTIFY_URL", "placeholder")
	t.Setenv("ONIBI_GOTIFY_APP_TOKEN", "app-token")
	t.Setenv("ONIBI_GOTIFY_CLIENT_TOKEN", "client-token")
	published := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/message"):
			writeDoctorJSON(t, w, map[string]any{"messages": []any{}})
		case strings.HasPrefix(r.URL.Path, "/stream"):
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			body := <-published
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(body))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/message"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			published <- body["message"].(string)
			writeDoctorJSON(t, w, map[string]any{"id": 1})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ONIBI_GOTIFY_URL", srv.URL)
	check := checkNamed(t, Run(t.Context(), Options{Paths: paths}), "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "send, WebSocket") {
		t.Fatalf("check = %#v", check)
	}
}

func withSlackFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newSlackClient
	newSlackClient = func(appToken, botToken string) *slack.Client {
		c := slack.New(appToken, botToken)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newSlackClient = old })
}

func withDiscordFactory(t *testing.T, baseURL string) {
	t.Helper()
	old := newDiscordClient
	newDiscordClient = func(token string) *discord.Client {
		c := discord.New(token)
		c.BaseURL = baseURL
		return c
	}
	t.Cleanup(func() { newDiscordClient = old })
}

func writeDoctorJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
