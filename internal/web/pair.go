package web

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
)

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := r.PathValue("token")
	if token == "" || s.db == nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", "missing_token_or_db", "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	if s.requireE2E {
		if s.relayKeys == nil {
			s.log.Warn("web relay key store unavailable", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))
			http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
			return
		}
		pairConfirmPage(w, token)
		return
	}
	claim, err := setup.Claim(r.Context(), s.db, token)
	if err != nil {
		s.log.Warn("web pair failed", "request_id", requestID(r), "reason", err.Error(), "remote", remoteHost(r.RemoteAddr), "user_agent", trimForLog(r.UserAgent(), 160))
		pairFailed(w)
		return
	}
	var sessionID string
	if claim.Role == store.PairRoleViewer {
		sessionID, err = s.CreateViewerSession(r.Context(), w, r.UserAgent(), claim.SessionID, claim.ExpiresAt)
	} else {
		sessionID, err = s.CreateWebSession(r.Context(), w, r.UserAgent(), claim.Role)
	}
	if err != nil {
		s.log.Error("web pair session create failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	if s.relayKeys != nil {
		bound, err := s.relayKeys.BindSession(r.Context(), s.db, token, sessionID)
		if err != nil {
			s.log.Warn("web relay key bind failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		}
		if s.requireE2E && !bound {
			s.log.Warn("web relay key missing", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))
			http.Error(w, "relay key missing", http.StatusUnauthorized)
			return
		}
	}
	s.log.Info("web pair accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr), "role", claim.Role, "user_agent", trimForLog(r.UserAgent(), 160))
	pairAccepted(w, claim.SessionID)
}

type pairConfirmRequest struct {
	Verifier string `json:"verifier"`
}

type pairConfirmResponse struct {
	SessionID string `json:"session_id"`
	Redirect  string `json:"redirect"`
}

func (s *Server) handlePairConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.requireE2E || s.relayKeys == nil || s.db == nil {
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return
	}
	body, err := envelope.ReadAllLimited(http.MaxBytesReader(w, r.Body, 1<<20+1), 1<<20)
	if err != nil {
		http.Error(w, "bad encrypted request", http.StatusBadRequest)
		return
	}
	if len(body) > 1<<20 {
		http.Error(w, "encrypted request too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !isE2EContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "bad encrypted request", http.StatusBadRequest)
		return
	}
	var frame envelope.RelayFrame
	if err := json.Unmarshal(body, &frame); err != nil || frame.SessionID == "" {
		http.Error(w, "bad encrypted request", http.StatusBadRequest)
		return
	}
	token := frame.SessionID
	key, ok := s.relayKeys.KeyForPair(token)
	if !ok {
		http.Error(w, "relay key missing", http.StatusUnauthorized)
		return
	}
	confirmKey := e2ecrypto.DerivePairConfirmKey(key, []byte(token))
	opened, plain, err := envelope.OpenRelayFrame(confirmKey, token, e2eInfoPairConfirm, e2eDirC2S, 0, body)
	if err != nil || opened.Type != e2eTypeText {
		http.Error(w, "bad encrypted request", http.StatusBadRequest)
		return
	}
	if !s.acceptE2EHTTPReplay(opened) {
		http.Error(w, "encrypted request replay", http.StatusConflict)
		return
	}
	var req pairConfirmRequest
	if err := json.Unmarshal(plain, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	got, err := base64.RawURLEncoding.DecodeString(req.Verifier)
	if err != nil {
		http.Error(w, "bad verifier", http.StatusUnauthorized)
		return
	}
	want, err := relayPairVerifier(key, token)
	if err != nil {
		http.Error(w, "bad verifier", http.StatusUnauthorized)
		return
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		http.Error(w, "bad verifier", http.StatusUnauthorized)
		return
	}
	claim, err := setup.Claim(r.Context(), s.db, token)
	if err != nil {
		s.log.Warn("web pair confirm failed", "request_id", requestID(r), "reason", err.Error(), "remote", remoteHost(r.RemoteAddr))
		pairFailed(w)
		return
	}
	var sessionID string
	if claim.Role == store.PairRoleViewer {
		sessionID, err = s.CreateViewerSession(r.Context(), w, r.UserAgent(), claim.SessionID, claim.ExpiresAt)
	} else {
		sessionID, err = s.CreateWebSession(r.Context(), w, r.UserAgent(), claim.Role)
	}
	if err != nil {
		s.log.Error("web pair confirm session create failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	bound, err := s.relayKeys.BindSession(r.Context(), s.db, token, sessionID)
	if err != nil {
		s.log.Warn("web relay key bind failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
	}
	if !bound {
		s.log.Warn("web relay key missing", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "relay key missing", http.StatusUnauthorized)
		return
	}
	redirect := "/"
	if claim.SessionID != "" {
		redirect = "/s/" + url.PathEscape(claim.SessionID)
	}
	plain, err = json.Marshal(pairConfirmResponse{SessionID: sessionID, Redirect: redirect})
	if err != nil {
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	sealed, err := envelope.SealRelayFrame(confirmKey, token, opened.StreamID, e2eInfoPairConfirm, e2eDirS2C, 0, e2eTypeText, plain)
	if err != nil {
		s.log.Error("web pair confirm response encrypt failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "pair failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", e2eContentType)
	s.log.Info("web pair accepted", "request_id", requestID(r), "remote", remoteHost(r.RemoteAddr), "role", claim.Role, "user_agent", trimForLog(r.UserAgent(), 160))
	_, _ = w.Write(sealed)
}

func pairFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, "pair token expired or already used")
}

func pairAccepted(w http.ResponseWriter, sessionID string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	target := "/"
	if sessionID != "" {
		target = "/s/" + url.PathEscape(sessionID)
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><title>Onibi paired</title><body><p>Paired. Opening Onibi...</p><script>const h=location.hash;location.replace(h.startsWith("#/")?h.slice(1):(h?"/"+h:%s))</script><p><a href="/">Open Onibi</a></p></body>`, strconv.Quote(target))
}

func pairConfirmPage(w http.ResponseWriter, token string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, `<!doctype html><title>Onibi pairing</title><body data-token=%s><p id="status">Pairing...</p><script>
(async function(){
const enc = new TextEncoder();
const dec = new TextDecoder();
const status = document.getElementById('status');
const token = document.body.dataset.token || '';
const channel = 'http:POST:/pair/confirm';
function fail(msg){ status.textContent = msg; }
function b64u(bytes){ let s = ''; bytes.forEach(function(b){ s += String.fromCharCode(b); }); return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, ''); }
function unb64u(value){ const padded = value.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(value.length / 4) * 4, '='); const bin = atob(padded); const out = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i += 1) out[i] = bin.charCodeAt(i); return out; }
function aad(frame){ return enc.encode([frame.v, frame.sid, frame.st, frame.ch, frame.dir, String(frame.seq), frame.iv, frame.t].join('\n')); }
function eq(a, b){ if (a.byteLength !== b.byteLength) return false; for (let i = 0; i < a.byteLength; i += 1) if (a[i] !== b[i]) return false; return true; }
function u64be(){ return new Uint8Array(8); }
async function deriveBits(base, salt, info, bits){ return crypto.subtle.deriveBits({name: 'HKDF', hash: 'SHA-256', salt: enc.encode(salt), info: enc.encode(info)}, base, bits); }
async function streamKey(base, stream, dir){ return crypto.subtle.deriveKey({name: 'HKDF', hash: 'SHA-256', salt: stream, info: enc.encode('onibi-e2e-stream-v1:' + channel + ':' + dir)}, base, {name: 'AES-GCM', length: 256}, false, ['encrypt', 'decrypt']); }
async function noncePrefix(base, stream, dir){ return new Uint8Array(await crypto.subtle.deriveBits({name: 'HKDF', hash: 'SHA-256', salt: stream, info: enc.encode('onibi-e2e-nonce-v1:' + channel + ':' + dir)}, base, 32)); }
const hash = location.hash.startsWith('#') ? location.hash.slice(1) : '';
const encoded = new URLSearchParams(hash).get('k');
if (encoded === null) { fail('Missing relay key. Rescan the QR.'); return; }
const raw = unb64u(encoded);
if (raw.byteLength !== 32) { fail('Bad relay key. Rescan the QR.'); return; }
history.replaceState(null, document.title, location.pathname + location.search);
const pairKey = await crypto.subtle.importKey('raw', raw, 'HKDF', false, ['deriveBits']);
const confirmBits = await deriveBits(pairKey, token, 'onibi-e2e-pair-confirm-v1', 256);
const confirmKey = await crypto.subtle.importKey('raw', confirmBits, 'HKDF', false, ['deriveKey', 'deriveBits']);
const verifier = new Uint8Array(await deriveBits(pairKey, token, 'onibi-e2e-pair-verifier-v1', 256));
const stream = crypto.getRandomValues(new Uint8Array(16));
const streamID = b64u(stream);
const aesKey = await streamKey(confirmKey, stream, 'c2s');
const prefix = await noncePrefix(confirmKey, stream, 'c2s');
const iv = new Uint8Array(12);
iv.set(prefix, 0);
iv.set(u64be(), 4);
const frame = {v: 'onibi.e2e.v1', sid: token, st: streamID, ch: channel, dir: 'c2s', seq: 0, iv: b64u(iv), t: 'text', ct: ''};
const plain = enc.encode(JSON.stringify({verifier: b64u(verifier)}));
const ct = await crypto.subtle.encrypt({name: 'AES-GCM', iv: iv, additionalData: aad(frame)}, aesKey, plain);
frame.ct = b64u(new Uint8Array(ct));
const res = await fetch('/pair/confirm', {method: 'POST', credentials: 'same-origin', headers: {'Content-Type': 'application/onibi-e2e+json'}, body: JSON.stringify(frame)});
if (!res.ok) { fail('Pairing failed. Rescan the QR.'); return; }
if ((res.headers.get('Content-Type') || '').split(';')[0].trim() !== 'application/onibi-e2e+json') { fail('Pairing failed. Rescan the QR.'); return; }
const outFrame = await res.json();
const outIV = unb64u(outFrame.iv || '');
const wantOutIV = new Uint8Array(12);
wantOutIV.set(await noncePrefix(confirmKey, stream, 's2c'), 0);
wantOutIV.set(u64be(), 4);
if (outFrame.v !== 'onibi.e2e.v1' || outFrame.sid !== token || outFrame.st !== streamID || outFrame.ch !== channel || outFrame.dir !== 's2c' || outFrame.seq !== 0 || outFrame.t !== 'text' || !eq(outIV, wantOutIV)) { fail('Pairing failed. Rescan the QR.'); return; }
const outPlain = await crypto.subtle.decrypt({name: 'AES-GCM', iv: outIV, additionalData: aad(outFrame)}, await streamKey(confirmKey, stream, 's2c'), unb64u(outFrame.ct || ''));
const out = JSON.parse(dec.decode(outPlain));
location.replace(out.redirect || '/');
})().catch(function(){ document.getElementById('status').textContent = 'Pairing failed. Rescan the QR.'; });
</script></body>`, strconv.Quote(token))
	_, _ = w.Write(b.Bytes())
}
