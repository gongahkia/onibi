package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/envelope"
)

const (
	e2eInfoPTY    = "ws:pty"
	e2eInfoEvents = "ws:events"
	e2eTypeText   = "text"
	e2eTypeBinary = "binary"
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
	return envelope.NewCodec(key, info)
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, ownerSessionID string, dst any) bool {
	codec, err := s.e2eCodec(ownerSessionID, "http:"+r.Method+":"+r.URL.Path)
	if err != nil {
		s.log.Warn("web e2e unavailable", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return false
	}
	body, err := envelope.ReadAllLimited(http.MaxBytesReader(w, r.Body, 1<<20), 1<<20)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	if len(body) > 1<<20 {
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

func wsDecrypt(codec *envelope.Codec, typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	if codec == nil {
		return typ, p, nil
	}
	if typ != websocket.MessageText {
		return typ, nil, errors.New("encrypted ws frame must be text")
	}
	frameType, plain, err := codec.Open(p, nil)
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
	if codec == nil {
		return typ, p, nil
	}
	frameType := e2eTypeText
	if typ == websocket.MessageBinary {
		frameType = e2eTypeBinary
	}
	out, err := codec.Seal(frameType, p, nil)
	if err != nil {
		return typ, nil, err
	}
	return websocket.MessageText, out, nil
}
