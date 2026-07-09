package email

import (
	"bytes"
	"errors"
	"net/smtp"
	"strings"
	"testing"
)

func TestSendBuildsSMTPMessage(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	c := New("smtp.example:587", "smtp.example", "user", "pass", "onibi@example.com")
	c.SendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		if auth == nil {
			t.Fatal("auth nil")
		}
		return nil
	}
	err := c.Send(t.Context(), Message{To: "owner@example.com", Subject: "Onibi approval", Body: "Approve: https://onibi.example/a"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAddr != "smtp.example:587" || gotFrom != "onibi@example.com" || len(gotTo) != 1 || gotTo[0] != "owner@example.com" {
		t.Fatalf("addr=%q from=%q to=%#v", gotAddr, gotFrom, gotTo)
	}
	for _, want := range []string{"From: onibi@example.com\r\n", "To: owner@example.com\r\n", "Subject: Onibi approval\r\n", "Content-Type: text/plain; charset=utf-8\r\n\r\nApprove:"} {
		if !bytes.Contains(gotMsg, []byte(want)) {
			t.Fatalf("msg = %q missing %q", string(gotMsg), want)
		}
	}
}

func TestSendAllowsNoAuth(t *testing.T) {
	c := New("localhost:1025", "", "", "", "onibi@example.com")
	c.SendMail = func(_ string, auth smtp.Auth, _ string, _ []string, _ []byte) error {
		if auth != nil {
			t.Fatal("auth should be nil")
		}
		return nil
	}
	if err := c.Send(t.Context(), Message{To: "owner@example.com", Subject: "Onibi approval", Body: "body"}); err != nil {
		t.Fatal(err)
	}
}

func TestSendSanitizesHeaders(t *testing.T) {
	c := New("localhost:1025", "", "", "", "onibi@example.com")
	c.SendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		if strings.Contains(string(msg), "\r\nBcc:") {
			t.Fatalf("msg = %q", string(msg))
		}
		if !strings.Contains(string(msg), "Subject: Hi Bcc: evil@example.com") {
			t.Fatalf("msg = %q", string(msg))
		}
		return nil
	}
	if err := c.Send(t.Context(), Message{To: "owner@example.com", Subject: "Hi\r\nBcc: evil@example.com", Body: "body"}); err != nil {
		t.Fatal(err)
	}
}

func TestSendReturnsSMTPError(t *testing.T) {
	c := New("localhost:1025", "", "", "", "onibi@example.com")
	c.SendMail = func(string, smtp.Auth, string, []string, []byte) error {
		return errors.New("smtp down")
	}
	err := c.Send(t.Context(), Message{To: "owner@example.com", Subject: "Onibi approval", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendValidatesRequiredFields(t *testing.T) {
	c := New("", "", "", "", "")
	if err := c.Send(t.Context(), Message{}); err == nil {
		t.Fatal("expected validation error")
	}
}
