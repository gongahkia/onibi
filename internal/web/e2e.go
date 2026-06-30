package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
)

const (
	e2eInfoPTY    = "ws:pty"
	e2eInfoEvents = "ws:events"
	e2eTypeText   = "text"
	e2eTypeBinary = "binary"
	e2eDirC2S     = "c2s"
	e2eDirS2C     = "s2c"
)

func (s *Server) e2eCodec(sessionID string, info string) (*envelope.Codec, error) {
	if s.relayKeys == nil {
		if s.requireE2E {
			return nil, errors.New("relay e2e key store unavailable")
		}
		return nil, nil
	}
	key, ok := s.relayKeys.KeyForSession(sessionID)
	if !ok {
		if s.requireE2E {
			return nil, errors.New("relay e2e key missing")
		}
		return nil, nil
	}
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(sessionID))
	return envelope.NewCodec(sessionKey, info)
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, ownerSessionID string, dst any) bool {
	return s.readJSONBodyLimit(w, r, ownerSessionID, dst, 1<<20)
}

func (s *Server) readJSONBodyLimit(w http.ResponseWriter, r *http.Request, ownerSessionID string, dst any, limit int64) bool {
	if limit <= 0 {
		limit = 1 << 20
	}
	codec, err := s.e2eCodec(ownerSessionID, "http:"+r.Method+":"+r.URL.Path)
	if err != nil {
		s.log.Warn("web e2e unavailable", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return false
	}
	body, err := envelope.ReadAllLimited(http.MaxBytesReader(w, r.Body, limit+1), limit)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	if int64(len(body)) > limit {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return false
	}
	if codec != nil {
		typ, plain, err := codec.Open(body, nil)
		if err != nil || typ != e2eTypeText {
			s.log.Warn("web e2e decrypt failed", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
			http.Error(w, "bad encrypted request", http.StatusBadRequest)
			return false
		}
		body = plain
	}
	if err := json.Unmarshal(body, dst); err != nil {
		s.log.Warn("web json bad request", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

type wsCodec interface {
	encrypt(websocket.MessageType, []byte) (websocket.MessageType, []byte, error)
	decrypt(websocket.MessageType, []byte) (websocket.MessageType, []byte, error)
}

type envelopeWSCodec struct {
	codec *envelope.Codec
}

func wrapWSCodec(codec *envelope.Codec) wsCodec {
	if codec == nil {
		return nil
	}
	return envelopeWSCodec{codec: codec}
}

func (c envelopeWSCodec) encrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	return wsEncrypt(c.codec, typ, p)
}

func (c envelopeWSCodec) decrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	return wsDecrypt(c.codec, typ, p)
}

type seqWSCodec struct {
	codec     *envelope.Codec
	sessionID string
	channel   string
	inDir     string
	outDir    string
	inSeq     uint64
	outSeq    uint64
}

func newSeqWSCodec(codec *envelope.Codec, sessionID, channel, inDir, outDir string) wsCodec {
	if codec == nil {
		return nil
	}
	return &seqWSCodec{codec: codec, sessionID: sessionID, channel: channel, inDir: inDir, outDir: outDir}
}

func (c *seqWSCodec) encrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	frameType := wsFrameType(typ)
	aad := wsFrameAAD(c.sessionID, c.channel, c.outDir, c.outSeq, frameType)
	outType, out, err := wsEncryptAAD(c.codec, typ, p, aad)
	if err != nil {
		return outType, out, err
	}
	c.outSeq++
	return outType, out, nil
}

func (c *seqWSCodec) decrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	frameType, err := encryptedWSFrameType(typ, p)
	if err != nil {
		return typ, nil, err
	}
	aad := wsFrameAAD(c.sessionID, c.channel, c.inDir, c.inSeq, frameType)
	outType, out, err := wsDecryptAAD(c.codec, typ, p, aad)
	if err != nil {
		return outType, out, err
	}
	c.inSeq++
	return outType, out, nil
}

func wsFrameAAD(sessionID, channel, dir string, seq uint64, frameType string) []byte {
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], seq)
	var b bytes.Buffer
	b.WriteString(sessionID)
	b.WriteByte(0)
	b.WriteString(channel)
	b.WriteByte(0)
	b.WriteString(dir)
	b.WriteByte(0)
	b.Write(seqBytes[:])
	b.WriteByte(0)
	b.WriteString(frameType)
	return b.Bytes()
}

func encryptedWSFrameType(typ websocket.MessageType, p []byte) (string, error) {
	if typ != websocket.MessageText {
		return "", errors.New("encrypted ws frame must be text")
	}
	var frame envelope.Frame
	if err := json.Unmarshal(p, &frame); err != nil {
		return "", err
	}
	if frame.Type != e2eTypeText && frame.Type != e2eTypeBinary {
		return "", fmt.Errorf("bad encrypted ws frame type %q", frame.Type)
	}
	return frame.Type, nil
}

func wsFrameType(typ websocket.MessageType) string {
	if typ == websocket.MessageBinary {
		return e2eTypeBinary
	}
	return e2eTypeText
}

func wsDecrypt(codec *envelope.Codec, typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	return wsDecryptAAD(codec, typ, p, nil)
}

func wsDecryptAAD(codec *envelope.Codec, typ websocket.MessageType, p []byte, aad []byte) (websocket.MessageType, []byte, error) {
	if codec == nil {
		return typ, p, nil
	}
	if typ != websocket.MessageText {
		return typ, nil, errors.New("encrypted ws frame must be text")
	}
	frameType, plain, err := codec.Open(p, aad)
	if err != nil {
		return typ, nil, err
	}
	switch frameType {
	case e2eTypeText:
		return websocket.MessageText, plain, nil
	case e2eTypeBinary:
		return websocket.MessageBinary, plain, nil
	default:
		return typ, nil, fmt.Errorf("bad encrypted ws frame type %q", frameType)
	}
}

func wsEncrypt(codec *envelope.Codec, typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	return wsEncryptAAD(codec, typ, p, nil)
}

func wsEncryptAAD(codec *envelope.Codec, typ websocket.MessageType, p []byte, aad []byte) (websocket.MessageType, []byte, error) {
	if codec == nil {
		return typ, p, nil
	}
	frameType := wsFrameType(typ)
	out, err := codec.Seal(frameType, p, aad)
	if err != nil {
		return typ, nil, err
	}
	return websocket.MessageText, out, nil
}
