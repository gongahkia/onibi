package pty

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
)

func TestTranscodeKittyGraphicsToIIP(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G'}
	payload := base64.StdEncoding.EncodeToString(raw)
	in := []byte("pre\x1b_Ga=T,f=100,s=1,v=1;" + payload + "\x1b\\post")
	want := []byte("pre\x1b]1337;File=inline=1;size=4:" + payload + "\x1b\\post")
	got := transcodeKittyGraphicsToIIP(in)
	if !bytes.Equal(got, want) {
		t.Fatalf("transcode = %q, want %q", got, want)
	}
}

func TestTranscodeKittyGraphicsRawBase64(t *testing.T) {
	raw := []byte{1, 2}
	payload := base64.RawStdEncoding.EncodeToString(raw)
	got := transcodeKittyGraphicsToIIP([]byte("\x1b_Ga=T,f=100;" + payload + "\x1b\\"))
	wantPayload := base64.StdEncoding.EncodeToString(raw)
	want := []byte("\x1b]1337;File=inline=1;size=2:" + wantPayload + "\x1b\\")
	if !bytes.Equal(got, want) {
		t.Fatalf("transcode = %q, want %q", got, want)
	}
}

func TestTranscodeKittyGraphicsMultipleAndMalformed(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("ok"))
	in := []byte("a\x1b_Ga=T,f=100;" + payload + "\x1b\\b\x1b_Ga=T,f=100;%%%%\x1b\\c\x1b_Ga=T,f=100;" + payload + "\x1b\\d")
	got := transcodeKittyGraphicsToIIP(in)
	wantImage := "\x1b]1337;File=inline=1;size=2:" + payload + "\x1b\\"
	want := []byte("a" + wantImage + "b\x1b_Ga=T,f=100;%%%%\x1b\\c" + wantImage + "d")
	if !bytes.Equal(got, want) {
		t.Fatalf("transcode = %q, want %q", got, want)
	}
}

func TestTranscodeKittyGraphicsPassthrough(t *testing.T) {
	for _, in := range [][]byte{
		[]byte("plain"),
		[]byte("\x1b_Ga=T,f=100;%%%%\x1b\\"),
		[]byte("\x1b_Ga=T,f=100"),
		[]byte("\x1b_Ga=T,f=100;\x1b\\"),
		[]byte("\x1b_Ga=T,f=32;" + base64.StdEncoding.EncodeToString([]byte("rgba")) + "\x1b\\"),
		[]byte("\x1b_Ga=T,f=100," + base64.StdEncoding.EncodeToString([]byte("missing semicolon")) + "\x1b\\"),
	} {
		got := transcodeKittyGraphicsToIIP(in)
		if !bytes.Equal(got, in) {
			t.Fatalf("transcode(%q) = %q", in, got)
		}
	}
}

func TestKittyGraphicsTranscoderSplitEnvelope(t *testing.T) {
	var tr kittyGraphicsTranscoder
	payload := base64.StdEncoding.EncodeToString([]byte("split"))
	first := tr.Write([]byte("pre\x1b_Ga=T,f=100;" + payload[:4]))
	if string(first) != "pre" {
		t.Fatalf("first write = %q", first)
	}
	second := tr.Write([]byte(payload[4:] + "\x1b\\post"))
	want := []byte("\x1b]1337;File=inline=1;size=5:" + payload + "\x1b\\post")
	if !bytes.Equal(second, want) {
		t.Fatalf("second write = %q, want %q", second, want)
	}
}

func TestKittyGraphicsTranscoderChunkedPNG(t *testing.T) {
	var tr kittyGraphicsTranscoder
	raw := []byte("chunked-png")
	payload := base64.StdEncoding.EncodeToString(raw)
	got := tr.Write([]byte("x\x1b_Ga=T,f=100,m=1;" + payload[:8] + "\x1b\\"))
	if string(got) != "x" {
		t.Fatalf("first chunk = %q", got)
	}
	got = tr.Write([]byte("\x1b_Gm=0;" + payload[8:] + "\x1b\\y"))
	want := []byte("\x1b]1337;File=inline=1;size=11:" + payload + "\x1b\\y")
	if !bytes.Equal(got, want) {
		t.Fatalf("final chunk = %q, want %q", got, want)
	}
}

func TestKittyGraphicsTranscoderMalformedChunkPassthrough(t *testing.T) {
	var tr kittyGraphicsTranscoder
	payload := base64.StdEncoding.EncodeToString([]byte("bad-chunk"))
	first := []byte("\x1b_Ga=T,f=100,m=1;" + payload[:8] + "\x1b\\")
	if got := tr.Write(first); len(got) != 0 {
		t.Fatalf("first chunk = %q", got)
	}
	second := []byte("\x1b_Ga=T,f=100;" + payload[8:] + "\x1b\\")
	got := tr.Write(second)
	want := append([]byte{}, first...)
	want = append(want, second...)
	if !bytes.Equal(got, want) {
		t.Fatalf("malformed chunk = %q, want %q", got, want)
	}
}

func TestKittyGraphicsTranscoderTmuxWrappedChunkedPNG(t *testing.T) {
	var tr kittyGraphicsTranscoder
	raw := []byte("tmux-chunk")
	payload := base64.StdEncoding.EncodeToString(raw)
	first := tmuxWrapKitty([]byte("\x1b_Ga=T,f=100,m=1;" + payload[:8] + "\x1b\\"))
	got := tr.Write(append([]byte("x"), first...))
	if string(got) != "x" {
		t.Fatalf("first wrapped chunk = %q", got)
	}
	second := tmuxWrapKitty([]byte("\x1b_Gm=0;" + payload[8:] + "\x1b\\"))
	got = tr.Write(append(second, 'y'))
	want := []byte("\x1b]1337;File=inline=1;size=10:" + payload + "\x1b\\y")
	if !bytes.Equal(got, want) {
		t.Fatalf("final wrapped chunk = %q, want %q", got, want)
	}
}

func TestKittyGraphicsTranscoderTmuxWrappedUnsupportedPassthrough(t *testing.T) {
	var tr kittyGraphicsTranscoder
	payload := base64.StdEncoding.EncodeToString([]byte("rgba"))
	in := tmuxWrapKitty([]byte("\x1b_Ga=T,f=32;" + payload + "\x1b\\"))
	got := tr.Write(in)
	if !bytes.Equal(got, in) {
		t.Fatalf("wrapped unsupported = %q, want %q", got, in)
	}
}

func TestHubWriteTranscodesKittyGraphics(t *testing.T) {
	h := NewHub(4096)
	defer h.Close()
	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()

	payload := base64.StdEncoding.EncodeToString([]byte("img"))
	in := []byte("pre\x1b_Ga=T,f=100;" + payload + "\x1b\\post")
	n, err := h.Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(in) {
		t.Fatalf("write length = %d, want %d", n, len(in))
	}
	got := readFrame(t, ch)
	if bytes.Contains(got, kittyGraphicsPrefix) {
		t.Fatalf("frame still contains kitty prefix: %q", got)
	}
	if !bytes.Contains(got, iipFilePrefix) {
		t.Fatalf("frame missing IIP prefix: %q", got)
	}
	replay := h.ReplaySince(0)
	if !bytes.Equal(replay.Data, got) {
		t.Fatalf("replay = %q, frame = %q", replay.Data, got)
	}
	if replay.Seq != uint64(len(got)) {
		t.Fatalf("seq = %d, want %d", replay.Seq, len(got))
	}
}

func tmuxWrapKitty(seq []byte) []byte {
	out := append([]byte{}, tmuxPassthrough...)
	for _, b := range seq {
		if b == 0x1b {
			out = append(out, 0x1b)
		}
		out = append(out, b)
	}
	out = append(out, stringTerminator...)
	return out
}

var benchmarkTranscodeSink []byte

func BenchmarkTranscodeKittyGraphicsToIIP(b *testing.B) {
	raw := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 4096)
	payload := base64.StdEncoding.EncodeToString(raw)
	input := []byte("\x1b_Ga=T,f=100,s=64,v=64;" + payload + "\x1b\\")
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for i := 0; i < b.N; i++ {
		benchmarkTranscodeSink = transcodeKittyGraphicsToIIP(input)
	}
}
