package zulip

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSendMessageShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("type") != "stream" || r.Form.Get("to") != "ops" || r.Form.Get("topic") != "onibi-s1" || r.Form.Get("content") != "tail" {
			t.Fatalf("form = %#v", r.Form)
		}
		writeZulipJSON(t, w, map[string]any{"result": "success", "msg": "", "id": 42})
	}))
	defer srv.Close()
	got, err := New(srv.URL, "bot@example.com", "key").SendStreamMessage(t.Context(), StreamMessage{Stream: "ops", Topic: "onibi-s1", Content: "tail"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 {
		t.Fatalf("id = %d", got.ID)
	}
}

func TestRegisterEventsReactionAndDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/register":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("event_types") != `["message","reaction"]` || r.Form.Get("apply_markdown") != "false" {
				t.Fatalf("form = %#v", r.Form)
			}
			var narrow [][]string
			if err := json.Unmarshal([]byte(r.Form.Get("narrow")), &narrow); err != nil {
				t.Fatal(err)
			}
			if len(narrow) != 1 || narrow[0][0] != "channel" || narrow[0][1] != "ops" {
				t.Fatalf("narrow = %#v", narrow)
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "queue_id": "q1", "last_event_id": 7, "event_queue_longpoll_timeout_seconds": 90})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/events":
			if r.URL.Query().Get("queue_id") != "q1" || r.URL.Query().Get("last_event_id") != "7" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "events": []any{map[string]any{
				"id": 8, "type": "message", "message": map[string]any{"id": 99, "type": "stream", "sender_email": "owner@example.com", "subject": "onibi-s1", "content": "pwd"},
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/messages/99/reactions":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("emoji_name") != "thumbs_up" {
				t.Fatalf("reaction form = %#v", r.Form)
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "msg": ""})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/events":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if values.Get("queue_id") != "q1" {
				t.Fatalf("delete form = %#v", values)
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "msg": ""})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "bot@example.com", "key")
	q, err := c.RegisterQueue(t.Context(), QueueOptions{EventTypes: []string{"message", "reaction"}, Narrow: [][]string{{"channel", "ops"}}})
	if err != nil {
		t.Fatal(err)
	}
	if q.QueueID != "q1" || q.LastEventID != 7 || q.LongPollTimeout != 90 {
		t.Fatalf("queue = %#v", q)
	}
	events, err := c.GetEvents(t.Context(), q.QueueID, q.LastEventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.Events[0].Message.Topic() != "onibi-s1" || events.Events[0].Message.Content != "pwd" {
		t.Fatalf("events = %#v", events)
	}
	if err := c.AddReaction(t.Context(), 99, "thumbs_up"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteQueue(t.Context(), "q1"); err != nil {
		t.Fatal(err)
	}
}

func TestTailEventsReconnects(t *testing.T) {
	registers := 0
	eventCalls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/register":
			registers++
			writeZulipJSON(t, w, map[string]any{"result": "success", "queue_id": fmt.Sprintf("q%d", registers), "last_event_id": 0})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/events":
			q := r.URL.Query().Get("queue_id")
			eventCalls[q]++
			if q == "q1" && eventCalls[q] == 2 {
				w.WriteHeader(http.StatusBadRequest)
				writeZulipJSON(t, w, map[string]any{"result": "error", "code": "BAD_EVENT_QUEUE_ID", "msg": "queue expired"})
				return
			}
			msg := "m1"
			if q == "q2" {
				msg = "m2"
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "events": []any{map[string]any{
				"id": eventCalls[q], "type": "message", "message": map[string]any{"id": eventCalls[q], "subject": "onibi-s1", "content": msg},
			}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/events":
			writeZulipJSON(t, w, map[string]any{"result": "success", "msg": ""})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	errStop := errors.New("stop")
	var got []string
	err := New(srv.URL, "bot@example.com", "key").TailEvents(t.Context(), TailOptions{RetryMin: time.Millisecond, RetryMax: time.Millisecond, MaxReconnects: 2}, func(ev Event) error {
		got = append(got, ev.Message.Content)
		if strings.Join(got, ",") == "m1,m2" {
			return errStop
		}
		return nil
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(got, ",") != "m1,m2" {
		t.Fatalf("got = %#v", got)
	}
}

func TestAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeZulipJSON(t, w, map[string]any{"result": "error", "code": "BAD_API_KEY", "msg": "bad key"})
	}))
	defer srv.Close()
	_, err := New(srv.URL, "bot@example.com", "key").SendStreamMessage(t.Context(), StreamMessage{Stream: "ops", Topic: "t", Content: "c"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "BAD_API_KEY" || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v", err)
	}
}

func assertAuth(t *testing.T, r *http.Request) {
	t.Helper()
	user, pass, ok := r.BasicAuth()
	if !ok || user != "bot@example.com" || pass != "key" {
		t.Fatalf("auth user=%q pass=%q ok=%v", user, pass, ok)
	}
}

func writeZulipJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}
