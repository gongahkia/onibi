package envelope

import "testing"

func FuzzCodecOpen(f *testing.F) {
	key := make([]byte, KeyBytes)
	codec, err := NewCodec(key, "ws:pty")
	if err != nil {
		f.Fatal(err)
	}
	sealed, err := codec.Seal("text", []byte("seed"), []byte("aad"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sealed, []byte("aad"))
	f.Add([]byte(`{"v":"onibi.e2e.v1"}`), []byte("aad"))
	f.Fuzz(func(t *testing.T, frame, aad []byte) {
		if len(frame) > 1<<20 || len(aad) > 1<<20 {
			t.Skip()
		}
		_, _, _ = codec.Open(frame, aad)
	})
}

func FuzzOpenRelayFrame(f *testing.F) {
	key := make([]byte, KeyBytes)
	streamID, err := NewStreamID()
	if err != nil {
		f.Fatal(err)
	}
	sealed, err := SealRelayFrame(key, "session", streamID, "ws:pty", "c2s", 0, "text", []byte("seed"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sealed)
	f.Add([]byte(`{"v":"onibi.e2e.v1","sid":"session"}`))
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > 1<<20 {
			t.Skip()
		}
		_, _, _ = OpenRelayFrame(key, "session", "ws:pty", "c2s", 0, frame)
	})
}
