package pty

import (
	"bytes"
	"encoding/base64"
	"strconv"
)

var (
	kittyGraphicsPrefix = []byte("\x1b_G")
	tmuxPassthrough     = []byte("\x1bPtmux;")
	stringTerminator    = []byte("\x1b\\")
	iipFilePrefix       = []byte("\x1b]1337;File=inline=1;size=")
)

const (
	maxKittyEnvelopeBytes = 8 << 20
	maxKittyImageBytes    = 32 << 20
)

type kittyGraphicsTranscoder struct {
	carry   []byte
	pending kittyGraphicsPending
}

type kittyGraphicsPending struct {
	active   bool
	original []byte
	data     []byte
}

func transcodeKittyGraphicsToIIP(p []byte) []byte {
	var t kittyGraphicsTranscoder
	out := t.Write(p)
	if len(t.carry) > 0 || t.pending.active {
		return p
	}
	return out
}

func (t *kittyGraphicsTranscoder) Write(p []byte) []byte {
	if len(p) == 0 {
		return nil
	}
	data := p
	if len(t.carry) > 0 {
		data = append(append([]byte{}, t.carry...), p...)
		t.carry = t.carry[:0]
	}
	_, _, ok := nextKittySequence(data, 0)
	if !ok {
		return data
	}
	out := make([]byte, 0, len(data))
	cursor := 0
	var start int
	var wrapped bool
	for {
		start, wrapped, ok = nextKittySequence(data, cursor)
		if !ok {
			break
		}
		if wrapped {
			out = append(out, data[cursor:start]...)
			inner, end, ok := unwrapTmuxPassthrough(data, start)
			if !ok {
				if len(data[start:]) > maxKittyEnvelopeBytes {
					out = append(out, data[start:]...)
					cursor = len(data)
					break
				}
				t.carry = append(t.carry, data[start:]...)
				return out
			}
			replacement, changed := t.transcodeCompleteKitty(inner, data[start:end])
			if changed {
				out = append(out, replacement...)
			} else {
				out = append(out, data[start:end]...)
			}
			cursor = end
			continue
		}
		bodyStart := start + len(kittyGraphicsPrefix)
		endRel := bytes.Index(data[bodyStart:], stringTerminator)
		if endRel < 0 {
			out = append(out, data[cursor:start]...)
			if len(data[start:]) > maxKittyEnvelopeBytes {
				out = append(out, data[start:]...)
				cursor = len(data)
				break
			}
			t.carry = append(t.carry, data[start:]...)
			return out
		}
		bodyEnd := bodyStart + endRel
		end := bodyEnd + len(stringTerminator)
		out = append(out, data[cursor:start]...)
		out = append(out, t.transcodeEnvelope(data[bodyStart:bodyEnd], data[start:end])...)
		cursor = end
	}
	out = append(out, data[cursor:]...)
	return out
}

func nextKittySequence(data []byte, cursor int) (int, bool, bool) {
	raw := bytes.Index(data[cursor:], kittyGraphicsPrefix)
	wrapped := bytes.Index(data[cursor:], tmuxPassthrough)
	if raw < 0 && wrapped < 0 {
		return 0, false, false
	}
	if wrapped >= 0 && (raw < 0 || wrapped < raw) {
		return cursor + wrapped, true, true
	}
	return cursor + raw, false, true
}

func unwrapTmuxPassthrough(data []byte, start int) ([]byte, int, bool) {
	bodyStart := start + len(tmuxPassthrough)
	if bodyStart > len(data) {
		return nil, 0, false
	}
	bodyEnd := -1
	for i := bodyStart; i < len(data)-1; i++ {
		if data[i] != 0x1b {
			continue
		}
		switch data[i+1] {
		case 0x1b:
			i++
		case '\\':
			bodyEnd = i
			i = len(data)
		}
	}
	if bodyEnd < 0 {
		return nil, 0, false
	}
	body := data[bodyStart:bodyEnd]
	inner := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		if body[i] == 0x1b && i+1 < len(body) && body[i+1] == 0x1b {
			inner = append(inner, 0x1b)
			i++
			continue
		}
		inner = append(inner, body[i])
	}
	return inner, bodyEnd + len(stringTerminator), true
}

func (t *kittyGraphicsTranscoder) transcodeCompleteKitty(seq, original []byte) ([]byte, bool) {
	if !bytes.HasPrefix(seq, kittyGraphicsPrefix) || !bytes.HasSuffix(seq, stringTerminator) {
		return nil, false
	}
	bodyStart := len(kittyGraphicsPrefix)
	bodyEnd := len(seq) - len(stringTerminator)
	out := t.transcodeEnvelope(seq[bodyStart:bodyEnd], original)
	if len(out) == 0 && t.pending.active {
		return nil, true
	}
	if bytes.Equal(out, original) {
		return nil, false
	}
	return out, true
}

func (t *kittyGraphicsTranscoder) transcodeEnvelope(body, original []byte) []byte {
	controlText, payload := splitKittyEnvelope(body)
	control := parseKittyControl(controlText)
	more := control["m"] == "1"
	if t.pending.active {
		t.pending.original = append(t.pending.original, original...)
		if !supportsKittyContinuation(control) {
			return t.flushPendingOriginal()
		}
		if len(payload) > 0 {
			decoded, ok := decodeKittyPayload(payload)
			if !ok || len(t.pending.data)+len(decoded) > maxKittyImageBytes {
				return t.flushPendingOriginal()
			}
			t.pending.data = append(t.pending.data, decoded...)
		}
		if more {
			return nil
		}
		return t.finishPending()
	}
	if !supportsDirectPNGKitty(control) {
		return original
	}
	if more {
		t.pending.active = true
		t.pending.original = append(t.pending.original[:0], original...)
		t.pending.data = t.pending.data[:0]
		if len(payload) > 0 {
			decoded, ok := decodeKittyPayload(payload)
			if !ok || len(decoded) > maxKittyImageBytes {
				return t.flushPendingOriginal()
			}
			t.pending.data = append(t.pending.data, decoded...)
		}
		return nil
	}
	if len(payload) == 0 {
		return original
	}
	decoded, ok := decodeKittyPayload(payload)
	if !ok {
		return original
	}
	return buildIIP(decoded)
}

func splitKittyEnvelope(body []byte) ([]byte, []byte) {
	semi := bytes.IndexByte(body, ';')
	if semi < 0 {
		return body, nil
	}
	return body[:semi], body[semi+1:]
}

func parseKittyControl(control []byte) map[string]string {
	items := bytes.Split(control, []byte{','})
	out := make(map[string]string, len(items))
	for _, item := range items {
		key, value, ok := bytes.Cut(item, []byte{'='})
		if !ok || len(key) == 0 {
			continue
		}
		out[string(key)] = string(value)
	}
	return out
}

func supportsDirectPNGKitty(control map[string]string) bool {
	if control["f"] != "100" {
		return false
	}
	if action := control["a"]; action != "" && action != "T" {
		return false
	}
	if compression := control["o"]; compression != "" {
		return false
	}
	if medium := control["t"]; medium != "" && medium != "d" {
		return false
	}
	return true
}

func supportsKittyContinuation(control map[string]string) bool {
	if _, ok := control["m"]; !ok {
		return false
	}
	for key := range control {
		if key != "m" && key != "q" {
			return false
		}
	}
	return true
}

func decodeKittyPayload(payload []byte) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(string(payload))
		if err != nil {
			return nil, false
		}
	}
	if len(decoded) == 0 {
		return nil, false
	}
	return decoded, true
}

func (t *kittyGraphicsTranscoder) flushPendingOriginal() []byte {
	out := append([]byte(nil), t.pending.original...)
	t.pending = kittyGraphicsPending{}
	return out
}

func (t *kittyGraphicsTranscoder) finishPending() []byte {
	original := t.pending.original
	decoded := t.pending.data
	t.pending = kittyGraphicsPending{}
	if len(decoded) == 0 {
		return original
	}
	return buildIIP(decoded)
}

func buildIIP(decoded []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(decoded)
	out := make([]byte, 0, len(iipFilePrefix)+20+1+len(encoded)+len(stringTerminator))
	out = append(out, iipFilePrefix...)
	out = strconv.AppendInt(out, int64(len(decoded)), 10)
	out = append(out, ':')
	out = append(out, encoded...)
	out = append(out, stringTerminator...)
	return out
}
