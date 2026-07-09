package email

import (
	"os"
	"testing"
)

func TestLiveSMTPEmail(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_EMAIL") != "1" {
		t.Skip("set ONIBI_LIVE_EMAIL=1")
	}
	c := New(os.Getenv("ONIBI_SMTP_ADDR"), os.Getenv("ONIBI_SMTP_HOST"), os.Getenv("ONIBI_SMTP_USERNAME"), os.Getenv("ONIBI_SMTP_PASSWORD"), os.Getenv("ONIBI_EMAIL_FROM"))
	if err := c.Send(t.Context(), Message{To: os.Getenv("ONIBI_EMAIL_TO"), Subject: "Onibi live email probe", Body: "onibi live email probe"}); err != nil {
		t.Fatal(err)
	}
}
