package envelope

import (
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	seed, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Minute)
	token, err := Encrypt(seed, Plain{Kind: "approval", ID: "abc", Title: "Approve", Body: "body"}, exp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(seed, token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "approval" || got.ID != "abc" || got.Body != "body" {
		t.Fatalf("bad round trip: %#v", got)
	}
}

func TestDecryptRejectsTamper(t *testing.T) {
	seed, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	token, err := Encrypt(seed, Plain{Kind: "approval", Body: "body"}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	tampered := token[:len(token)-1] + "A"
	if tampered == token {
		tampered = token[:len(token)-1] + "B"
	}
	if _, err := Decrypt(seed, tampered, time.Now()); err == nil {
		t.Fatal("expected tamper error")
	}
}

func TestDecryptRejectsExpired(t *testing.T) {
	seed, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	token, err := Encrypt(seed, Plain{Kind: "approval", Body: "body"}, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(seed, token, time.Now()); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestBuildMiniAppURL(t *testing.T) {
	got, err := BuildMiniAppURL("https://example.com/onibi/", "a+b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://example.com/onibi/#onibi=") {
		t.Fatalf("bad url: %s", got)
	}
	if _, err := BuildMiniAppURL("http://example.com", "x"); err == nil {
		t.Fatal("expected https error")
	}
}
