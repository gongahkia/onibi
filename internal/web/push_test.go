package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/webpush"
)

const testPushBody = `{"endpoint":"https://web.push.apple.com/abc","expirationTime":null,"keys":{"p256dh":"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4","auth":"BTBZMqHH6r4Tts7J_aSIgg"}}`

type sentPush struct {
	endpoint string
	msg      notification
}

func recordPushes(s *Server) (*[]sentPush, *sync.Mutex, *error) {
	var mu sync.Mutex
	var sent []sentPush
	var failure error
	s.pushSend = func(_ context.Context, sub webpush.Subscription, payload []byte) error {
		mu.Lock()
		defer mu.Unlock()
		var n notification
		_ = json.Unmarshal(payload, &n)
		sent = append(sent, sentPush{sub.Endpoint, n})
		return failure
	}
	return &sent, &mu, &failure
}

func TestPushSubscriptionFollowsTheBrowserGrant(t *testing.T) {
	s, _, _, b := testSessionServer(t)
	sent, _, _ := recordPushes(s)
	status := request(s, b, http.MethodGet, "/api/push", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"subscribed":false`) || !strings.Contains(status.Body.String(), `"publicKey":"B`) {
		t.Fatalf("status=%d %s", status.Code, status.Body)
	}
	if got := request(s, b, http.MethodPost, "/api/push/subscribe", []byte(strings.Replace(testPushBody, "web.push.apple.com", "attacker.example", 1))); got.Code != http.StatusBadRequest {
		t.Fatalf("foreign endpoint=%d", got.Code)
	}
	if got := request(s, b, http.MethodPost, "/api/push/subscribe", []byte(testPushBody)); got.Code != http.StatusOK {
		t.Fatalf("subscribe=%d %s", got.Code, got.Body)
	}
	if got := request(s, b, http.MethodGet, "/api/push", nil); !strings.Contains(got.Body.String(), `"subscribed":true`) {
		t.Fatalf("status after subscribe: %s", got.Body)
	}
	if got := request(s, b, http.MethodPost, "/api/push/test", []byte("{}")); got.Code != http.StatusOK || len(*sent) != 1 || (*sent)[0].msg.Tag != "onibi-test" {
		t.Fatalf("test=%d %s sent=%v", got.Code, got.Body, *sent)
	}
	devices, err := s.ListDevices(t.Context())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices=%v err=%v", devices, err)
	}
	if err := s.RevokeDevice(t.Context(), devices[0].ID); err != nil {
		t.Fatal(err)
	}
	if subs, err := s.pushSubscriptions(t.Context(), ""); err != nil || len(subs) != 0 {
		t.Fatalf("revoked device kept its subscription: %v err=%v", subs, err)
	}
}

func TestNotifierAnnouncesOnlyNewWaitsWithoutTerminalText(t *testing.T) {
	s, _, managed, b := testSessionServer(t)
	sent, mu, failure := recordPushes(s)
	if got := request(s, b, http.MethodPost, "/api/push/subscribe", []byte(testPushBody)); got.Code != http.StatusOK {
		t.Fatalf("subscribe=%d", got.Code)
	}
	clock := time.Now()
	s.now = func() time.Time { return clock }
	set := func(state, reason string) {
		managed.mu.Lock()
		managed.sessions = []intake.SessionInfo{{ID: testSessionID, Name: "build", Attention: intake.Attention{State: state, Reason: reason, Since: clock}}}
		managed.mu.Unlock()
	}
	n := &notifier{seen: map[string]attentionMark{}, lastSent: map[string]time.Time{}}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(*sent) }

	set("input", "Yes/no prompt")
	s.pollNotifications(t.Context(), n)
	if count() != 0 {
		t.Fatal("a wait that existed before the watcher started was announced")
	}
	clock = clock.Add(time.Minute)
	set("active", "")
	s.pollNotifications(t.Context(), n)
	clock = clock.Add(time.Minute)
	set("input", "Question prompt")
	s.pollNotifications(t.Context(), n)
	s.pollNotifications(t.Context(), n)
	if count() != 1 || (*sent)[0].msg != (notification{Title: "build needs input", Body: "Question prompt", Tag: "onibi-session-" + testSessionID, URL: "/?session=" + testSessionID}) {
		t.Fatalf("sent=%+v", *sent)
	}
	clock = clock.Add(5 * time.Second)
	set("bell", "Terminal bell")
	s.pollNotifications(t.Context(), n)
	if count() != 1 {
		t.Fatal("cooldown ignored")
	}
	clock = clock.Add(time.Minute)
	set("active", "")
	s.pollNotifications(t.Context(), n)
	clock = clock.Add(time.Second)
	set("bell", "Terminal bell")
	*failure = webpush.ErrGone
	s.pollNotifications(t.Context(), n)
	if count() != 2 || (*sent)[1].msg.Title != "build rang the bell" {
		t.Fatalf("sent=%+v", *sent)
	}
	if subs, _ := s.pushSubscriptions(t.Context(), ""); len(subs) != 0 {
		t.Fatal("a gone subscription was kept")
	}
}
