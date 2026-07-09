package sms

import (
	"os"
	"testing"
)

func TestLiveTwilioSMS(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_SMS") != "1" {
		t.Skip("set ONIBI_LIVE_SMS=1")
	}
	c := New(os.Getenv("ONIBI_TWILIO_ACCOUNT_SID"), os.Getenv("ONIBI_TWILIO_AUTH_TOKEN"), os.Getenv("ONIBI_TWILIO_FROM"), os.Getenv("ONIBI_TWILIO_MESSAGING_SERVICE_SID"))
	resp, err := c.Send(t.Context(), Message{To: os.Getenv("ONIBI_SMS_TO"), Body: "onibi live sms probe"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SID == "" {
		t.Fatalf("missing sid: %#v", resp)
	}
}
