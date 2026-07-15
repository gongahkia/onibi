package fleet

import (
	"encoding/json"
	"testing"
)

func FuzzEnrollmentChallengeValidate(f *testing.F) {
	f.Add([]byte(`{"version":1,"id":"enroll-123","owner_id":"owner-123","nonce":"nonce","hub_public":"key","expires_at":"2026-07-15T00:00:00Z"}`))
	f.Add([]byte(`{"version":1}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			t.Skip()
		}
		var challenge EnrollmentChallenge
		if json.Unmarshal(raw, &challenge) == nil {
			_ = challenge.Validate()
		}
	})
}
