package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
)

const (
	e2eInfoPTY         = "ws:pty"
	e2eInfoEvents      = "ws:events"
	e2eInfoPairConfirm = "http:POST:/pair/confirm"
	e2eTypeText        = "text"
	e2eTypeBinary      = "binary"
	e2eDirC2S          = "c2s"
	e2eDirS2C          = "s2c"
	e2eContentType     = "application/onibi-e2e+json"
	e2eOldContentType  = "application/vnd.onibi.e2e+json"
	e2eHTTPReplayTTL   = 10 * time.Minute
	e2eHTTPReplayLimit = 4096
)

var e2eNow = time.Now

type e2eHTTPMeta struct {
	sessionKey []byte
	sessionID  string
	streamID   string
	channel    string
}

func (s *Server) e2eSessionKey(sessionID string) ([]byte, error) {
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
	return e2ecrypto.DeriveSessionKey(key, []byte(sessionID)), nil
}

func (s *Server) e2eHTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isE2EContentType(r.Header.Get("Content-Type")) {
			next.ServeHTTP(w, r)
			return
		}
		bw := newBufferedResponseWriter()
		next.ServeHTTP(bw, r)
		meta, ok := s.takeE2EHTTPResponse(r)
		if !ok {
			bw.flushTo(w)
			return
		}
		status := bw.statusCode()
		if status == http.StatusNoContent {
			status = http.StatusOK
		}
		if status == http.StatusNotModified {
			bw.flushTo(w)
			return
		}
		sealed, err := envelope.SealRelayFrame(meta.sessionKey, meta.sessionID, meta.streamID, meta.channel, e2eDirS2C, 0, e2eTypeText, bw.body.Bytes())
		if err != nil {
			s.log.Warn("web e2e response encrypt failed", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
			http.Error(w, "relay e2e response failed", http.StatusInternalServerError)
			return
		}
		copyHeader(w.Header(), bw.header)
		w.Header().Set("Content-Type", e2eContentType)
		w.Header().Del("Content-Length")
		w.WriteHeader(status)
		_, _ = w.Write(sealed)
	})
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, ownerSessionID string, dst any) bool {
	return s.readJSONBodyLimit(w, r, ownerSessionID, dst, 1<<20)
}

func (s *Server) readJSONBodyLimit(w http.ResponseWriter, r *http.Request, ownerSessionID string, dst any, limit int64) bool {
	if limit <= 0 {
		limit = 1 << 20
	}
	sessionKey, err := s.e2eSessionKey(ownerSessionID)
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
	encrypted := isE2EContentType(r.Header.Get("Content-Type"))
	if encrypted {
		if sessionKey == nil {
			http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
			return false
		}
		channel := "http:" + r.Method + ":" + r.URL.Path
		frame, plain, err := envelope.OpenRelayFrame(sessionKey, ownerSessionID, channel, e2eDirC2S, 0, body)
		if err != nil || frame.Type != e2eTypeText {
			s.log.Warn("web e2e decrypt failed", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
			http.Error(w, "bad encrypted request", http.StatusBadRequest)
			return false
		}
		if !s.acceptE2EHTTPReplay(frame) {
			http.Error(w, "encrypted request replay", http.StatusConflict)
			return false
		}
		s.storeE2EHTTPResponse(r, e2eHTTPMeta{
			sessionKey: append([]byte(nil), sessionKey...),
			sessionID:  ownerSessionID,
			streamID:   frame.StreamID,
			channel:    channel,
		})
		body = plain
	} else if sessionKey != nil && s.requireE2E {
		http.Error(w, "bad encrypted request", http.StatusBadRequest)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		s.log.Warn("web json bad request", "request_id", requestID(r), "path", safeRequestPath(r.URL.Path), "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) acceptE2EHTTPReplay(frame envelope.RelayFrame) bool {
	now := e2eNow()
	key := strings.Join([]string{
		frame.SessionID,
		frame.StreamID,
		frame.Channel,
		frame.Direction,
		fmt.Sprintf("%d", frame.Seq),
	}, "\x00")
	s.e2eMu.Lock()
	defer s.e2eMu.Unlock()
	if s.e2eHTTPReplay == nil {
		s.e2eHTTPReplay = map[string]time.Time{}
	}
	for k, expires := range s.e2eHTTPReplay {
		if !expires.After(now) {
			delete(s.e2eHTTPReplay, k)
		}
	}
	if expires, ok := s.e2eHTTPReplay[key]; ok && expires.After(now) {
		return false
	}
	for len(s.e2eHTTPReplay) >= e2eHTTPReplayLimit {
		for k := range s.e2eHTTPReplay {
			delete(s.e2eHTTPReplay, k)
			break
		}
	}
	s.e2eHTTPReplay[key] = now.Add(e2eHTTPReplayTTL)
	return true
}

func (s *Server) storeE2EHTTPResponse(r *http.Request, meta e2eHTTPMeta) {
	s.e2eMu.Lock()
	defer s.e2eMu.Unlock()
	if s.e2eHTTPResponse == nil {
		s.e2eHTTPResponse = map[*http.Request]e2eHTTPMeta{}
	}
	s.e2eHTTPResponse[r] = meta
}

func (s *Server) takeE2EHTTPResponse(r *http.Request) (e2eHTTPMeta, bool) {
	s.e2eMu.Lock()
	defer s.e2eMu.Unlock()
	meta, ok := s.e2eHTTPResponse[r]
	if ok {
		delete(s.e2eHTTPResponse, r)
	}
	return meta, ok
}

type wsCodec interface {
	encrypt(websocket.MessageType, []byte) (websocket.MessageType, []byte, error)
	decrypt(websocket.MessageType, []byte) (websocket.MessageType, []byte, error)
}

type seqWSCodec struct {
	sessionKey []byte
	sessionID  string
	channel    string
	streamID   string
	inDir      string
	outDir     string
	inSeq      uint64
	outSeq     uint64
}

func newSeqWSCodec(sessionKey []byte, sessionID, channel, inDir, outDir string) wsCodec {
	if sessionKey == nil {
		return nil
	}
	return &seqWSCodec{
		sessionKey: append([]byte(nil), sessionKey...),
		sessionID:  sessionID,
		channel:    channel,
		inDir:      inDir,
		outDir:     outDir,
	}
}

func newSeqWSClientCodec(sessionKey []byte, sessionID, channel, inDir, outDir string) (wsCodec, error) {
	if sessionKey == nil {
		return nil, nil
	}
	streamID, err := envelope.NewStreamID()
	if err != nil {
		return nil, err
	}
	return &seqWSCodec{
		sessionKey: append([]byte(nil), sessionKey...),
		sessionID:  sessionID,
		channel:    channel,
		streamID:   streamID,
		inDir:      inDir,
		outDir:     outDir,
	}, nil
}

func (c *seqWSCodec) encrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	if c.streamID == "" {
		return typ, nil, errors.New("e2e stream not established")
	}
	frameType := wsFrameType(typ)
	out, err := envelope.SealRelayFrame(c.sessionKey, c.sessionID, c.streamID, c.channel, c.outDir, c.outSeq, frameType, p)
	if err != nil {
		return typ, nil, err
	}
	c.outSeq++
	return websocket.MessageText, out, nil
}

func (c *seqWSCodec) decrypt(typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	if typ != websocket.MessageText {
		return typ, nil, errors.New("encrypted ws frame must be text")
	}
	frame, plain, err := envelope.OpenRelayFrame(c.sessionKey, c.sessionID, c.channel, c.inDir, c.inSeq, p)
	if err != nil {
		return typ, nil, err
	}
	if c.streamID == "" {
		c.streamID = frame.StreamID
	} else if frame.StreamID != c.streamID {
		return typ, nil, errors.New("bad e2e ws stream")
	}
	c.inSeq++
	switch frame.Type {
	case e2eTypeText:
		return websocket.MessageText, plain, nil
	case e2eTypeBinary:
		return websocket.MessageBinary, plain, nil
	default:
		return typ, nil, fmt.Errorf("bad encrypted ws frame type %q", frame.Type)
	}
}

func wsFrameType(typ websocket.MessageType) string {
	if typ == websocket.MessageBinary {
		return e2eTypeBinary
	}
	return e2eTypeText
}

func wsDecrypt(codec wsCodec, typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	if codec == nil {
		return typ, p, nil
	}
	return codec.decrypt(typ, p)
}

func wsEncrypt(codec wsCodec, typ websocket.MessageType, p []byte) (websocket.MessageType, []byte, error) {
	if codec == nil {
		return typ, p, nil
	}
	return codec.encrypt(typ, p)
}

func isE2EContentType(value string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	return base == e2eContentType || base == e2eOldContentType
}

type bufferedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: http.Header{}}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *bufferedResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *bufferedResponseWriter) flushTo(dst http.ResponseWriter) {
	copyHeader(dst.Header(), w.header)
	dst.WriteHeader(w.statusCode())
	_, _ = dst.Write(w.body.Bytes())
}

func copyHeader(dst, src http.Header) {
	for k, values := range src {
		for _, value := range values {
			dst.Add(k, value)
		}
	}
}
