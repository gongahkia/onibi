package signal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParityAxes(t *testing.T) {
	t.Run("rpc send reaction subscribe", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/check":
				if r.Method != http.MethodGet {
					t.Fatalf("check method = %s", r.Method)
				}
				w.WriteHeader(http.StatusOK)
			case "/api/v1/rpc":
				var req rpcTestRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if req.JSONRPC != "2.0" || req.ID == nil {
					t.Fatalf("rpc envelope = %#v", req)
				}
				switch req.Method {
				case "send":
					assertParam(t, req.Params, "account", "+111")
					assertParam(t, req.Params, "message", "approve a1")
					assertRecipients(t, req.Params, "+222")
					writeRPCResult(t, w, req.ID, SendResult{Timestamp: 123})
				case "sendReaction":
					assertParam(t, req.Params, "account", "+111")
					assertParam(t, req.Params, "targetAuthor", "+222")
					assertParam(t, req.Params, "targetTimestamp", float64(123))
					assertParam(t, req.Params, "emoji", "\U0001f44d")
					assertRecipients(t, req.Params, "+222")
					writeRPCResult(t, w, req.ID, SendResult{Timestamp: 124})
				case "subscribeReceive":
					assertParam(t, req.Params, "account", "+111")
					writeRPCResult(t, w, req.ID, 42)
				default:
					t.Fatalf("method = %s", req.Method)
				}
			default:
				t.Fatalf("path = %s", r.URL.Path)
			}
		}))
		defer srv.Close()
		c := New(srv.URL, "+111")
		if err := c.Check(t.Context()); err != nil {
			t.Fatal(err)
		}
		sent, err := c.Send(t.Context(), SendRequest{Recipients: []string{"+222"}, Message: "approve a1"})
		if err != nil {
			t.Fatal(err)
		}
		if sent.Timestamp != 123 {
			t.Fatalf("sent = %#v", sent)
		}
		reacted, err := c.SendReaction(t.Context(), ReactionRequest{Recipients: []string{"+222"}, Emoji: "\U0001f44d", TargetAuthor: "+222", TargetTimestamp: 123})
		if err != nil {
			t.Fatal(err)
		}
		if reacted.Timestamp != 124 {
			t.Fatalf("reacted = %#v", reacted)
		}
		sub, err := c.SubscribeReceive(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if sub != 42 {
			t.Fatalf("subscription = %d", sub)
		}
	})
	t.Run("events parse chunked messages", func(t *testing.T) {
		direct := `{"jsonrpc":"2.0","method":"receive","params":{"account":"+111","envelope":{"source":"+222","timestamp":1,"dataMessage":{"timestamp":1,"message":"approve a1"}}}}`
		wrapped := `{"jsonrpc":"2.0","method":"receive","params":{"result":{"account":"+111","subscription":7,"envelope":{"source":"+333","timestamp":2,"dataMessage":{"timestamp":2,"message":"deny a1"}}}}}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/events" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			flusher, _ := w.(http.Flusher)
			fmt.Fprint(w, "data: "+direct[:len(direct)/2])
			flusher.Flush()
			fmt.Fprint(w, direct[len(direct)/2:]+"\n\n")
			fmt.Fprint(w, "data: "+wrapped+"\n\n")
		}))
		defer srv.Close()
		var got []string
		err := New(srv.URL, "").Events(t.Context(), func(ev Event) error {
			if ev.Method != "receive" {
				t.Fatalf("method = %s", ev.Method)
			}
			got = append(got, ev.Envelope.DataMessage.Message)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != "approve a1,deny a1" {
			t.Fatalf("got = %#v", got)
		}
	})
	t.Run("tail reconnect", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			fmt.Fprintf(w, `data: {"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+222","timestamp":%d,"dataMessage":{"timestamp":%d,"message":"m%d"}}}}`+"\n\n", calls, calls, calls)
		}))
		defer srv.Close()
		errStop := errors.New("stop")
		var got []string
		err := New(srv.URL, "").TailEvents(t.Context(), TailOptions{RetryMin: time.Millisecond, RetryMax: time.Millisecond, MaxReconnects: 2}, func(ev Event) error {
			got = append(got, ev.Envelope.DataMessage.Message)
			if len(got) == 2 {
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
	})
	t.Run("rpc error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeRPCError(t, w, 1, RPCError{Code: -32602, Message: "bad params"})
		}))
		defer srv.Close()
		err := New(srv.URL, "").Call(t.Context(), "send", map[string]any{}, nil)
		var rpcErr *RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 {
			t.Fatalf("err = %v", err)
		}
	})
}

type rpcTestRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
	ID      any            `json:"id"`
}

func assertParam(t *testing.T, params map[string]any, key string, want any) {
	t.Helper()
	if params[key] != want {
		t.Fatalf("%s = %#v want %#v", key, params[key], want)
	}
}

func assertRecipients(t *testing.T, params map[string]any, want string) {
	t.Helper()
	recipients, ok := params["recipient"].([]any)
	if !ok || len(recipients) != 1 || recipients[0] != want {
		t.Fatalf("recipient = %#v", params["recipient"])
	}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id any, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": result, "id": id}); err != nil {
		t.Fatal(err)
	}
}

func writeRPCError(t *testing.T, w http.ResponseWriter, id any, rpcErr RPCError) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "error": rpcErr, "id": id}); err != nil {
		t.Fatal(err)
	}
}
