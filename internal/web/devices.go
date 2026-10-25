package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const rememberedLifetime = 30 * 24 * time.Hour

// Device contains display metadata only. Credentials are never returned by the
// device API; the database holds only a hash of the HttpOnly cookie secret.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Remembered bool      `json:"remembered"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func (s *Server) prepareDevices(ctx context.Context) error {
	_, err := s.db.SQL().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS web_devices (
 id TEXT PRIMARY KEY, origin TEXT NOT NULL, name TEXT NOT NULL,
 token_hash BLOB UNIQUE, remembered INTEGER NOT NULL,
 created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
 )`)
	if err != nil {
		return err
	}
	// Temporary devices never survive a server restart. Remembered devices have
	// a fixed expiry: reconnecting does not silently extend the 30-day grant.
	_, err = s.db.SQL().ExecContext(ctx, `DELETE FROM web_devices WHERE remembered=0 OR expires_at<=?`, s.now().Unix())
	return err
}

// FreshPairingURL rotates only the unconsumed grant, never existing devices.
// Call only from the same-user local administration channel.
func (s *Server) FreshPairingURL() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	link, _ := url.Parse(s.origin)
	link.Path, link.Fragment = "/", "token="+token
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairHash, s.pairExpires, s.pairUsed, s.pairURL = sha256.Sum256([]byte(token)), s.now().Add(pairingLifetime), false, ""
	s.log.Info("web.pairing_link_issued", "expires_in_minutes", 10)
	return link.String(), nil
}

func (s *Server) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `SELECT id,name,remembered,created_at,expires_at FROM web_devices WHERE origin=? AND expires_at>? ORDER BY created_at,id`, s.origin, s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		var d Device
		var created, expires int64
		if err := rows.Scan(&d.ID, &d.Name, &d.Remembered, &created, &expires); err != nil {
			return nil, err
		}
		d.CreatedAt, d.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Server) RevokeDevice(ctx context.Context, id string) error {
	if !validReceiptID(id) {
		return errors.New("invalid device ID")
	}
	_, err := s.db.SQL().ExecContext(ctx, `DELETE FROM web_devices WHERE id=? AND origin=?`, id, s.origin)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.Info("web.device_revoked")
	for key, session := range s.sessions {
		if session.Device.ID == id {
			delete(s.sessions, key)
		}
	}
	return nil
}

func deviceName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "This browser"
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 64 {
		return "", false
	}
	for _, ch := range name {
		if unicode.IsControl(ch) {
			return "", false
		}
	}
	return name, true
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Remember bool   `json:"remember"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	name, valid := deviceName(body.Name)
	if !valid {
		writeError(w, 400, "invalid_device_name", "Use a device name of up to 64 characters without control characters.")
		return
	}
	hash := sha256.Sum256([]byte(body.Token))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairUsed || !s.now().Before(s.pairExpires) || subtle.ConstantTimeCompare(hash[:], s.pairHash[:]) != 1 {
		writeError(w, 401, "invalid_pairing", "This link is invalid, expired, or already used. Run onibi web pair on your Mac for a fresh link.")
		return
	}
	id, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	secret, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	sessionToken, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	csrf, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	lifetime := sessionLifetime
	if body.Remember {
		lifetime = rememberedLifetime
	}
	d := Device{ID: id, Name: name, Remembered: body.Remember, CreatedAt: s.now().UTC(), ExpiresAt: s.now().Add(lifetime).UTC()}

	// Keep expired enrollment metadata and session entries from accumulating.
	if _, err := s.db.SQL().ExecContext(r.Context(), `DELETE FROM web_devices WHERE expires_at<=?`, s.now().Unix()); err != nil {
		deviceFailure(w)
		return
	}
	for key, old := range s.sessions {
		if !s.now().Before(old.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
	secretHash := sha256.Sum256([]byte(secret))
	var storedHash any
	if body.Remember {
		storedHash = secretHash[:]
	}
	_, err = s.db.SQL().ExecContext(r.Context(), `INSERT INTO web_devices(id,origin,name,token_hash,remembered,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, id, s.origin, name, storedHash, body.Remember, d.CreatedAt.Unix(), d.ExpiresAt.Unix())
	if err != nil {
		deviceFailure(w)
		return
	}
	session := browserSession{CSRF: csrf, ExpiresAt: s.now().Add(sessionLifetime), Device: d}
	s.sessions[sha256.Sum256([]byte(sessionToken))] = session
	s.pairUsed, s.pairURL = true, ""
	s.log.Info("web.browser_paired", "remembered", body.Remember)
	s.setCookie(w, sessionToken, session.ExpiresAt)
	if body.Remember {
		s.setDeviceCookie(w, secret, d.ExpiresAt)
	} else if _, err := r.Cookie(s.deviceCookieName()); err == nil {
		s.setDeviceCookie(w, "", time.Unix(1, 0))
	}
	writeJSON(w, 200, session)
}

func (s *Server) authenticate(r *http.Request) (browserSession, [sha256.Size]byte, bool) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil || len(cookie.Value) != 43 {
		return browserSession{}, [sha256.Size]byte{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	session, ok := s.sessions[key]
	if ok && !s.now().Before(session.ExpiresAt) {
		delete(s.sessions, key)
		ok = false
	}
	s.mu.Unlock()
	if ok {
		var exists int
		err := s.db.SQL().QueryRowContext(r.Context(), `SELECT 1 FROM web_devices WHERE id=? AND origin=? AND expires_at>?`, session.Device.ID, s.origin, s.now().Unix()).Scan(&exists)
		ok = err == nil
	}
	return session, key, ok
}

// Only a GET to the session endpoint may exchange a remembered credential for
// a fresh short session/CSRF token. An expired POST is rejected, never replayed.
func (s *Server) resumeSession(w http.ResponseWriter, r *http.Request) {
	if session, _, ok := s.authenticate(r); ok {
		writeJSON(w, 200, session)
		return
	}
	cookie, err := r.Cookie(s.deviceCookieName())
	if err != nil || len(cookie.Value) != 43 {
		writeError(w, 401, "unauthorized", "Pair this browser with Onibi to continue.")
		return
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	var d Device
	var created, expires int64
	err = s.db.SQL().QueryRowContext(r.Context(), `SELECT id,name,remembered,created_at,expires_at FROM web_devices WHERE token_hash=? AND origin=? AND remembered=1 AND expires_at>?`, hash[:], s.origin, s.now().Unix()).Scan(&d.ID, &d.Name, &d.Remembered, &created, &expires)
	if err != nil {
		writeError(w, 401, "unauthorized", "This browser is no longer remembered. Pair again using onibi web pair on your Mac.")
		return
	}
	d.CreatedAt, d.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	token, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	csrf, err := randomToken()
	if err != nil {
		deviceFailure(w)
		return
	}
	expiry := s.now().Add(sessionLifetime)
	if expiry.After(d.ExpiresAt) {
		expiry = d.ExpiresAt
	}
	session := browserSession{CSRF: csrf, ExpiresAt: expiry, Device: d}
	s.mu.Lock()
	for key, old := range s.sessions {
		if !s.now().Before(old.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
	s.sessions[sha256.Sum256([]byte(token))] = session
	s.mu.Unlock()
	s.setCookie(w, token, expiry)
	writeJSON(w, 200, session)
}

func (s *Server) deviceCookieName() string {
	if s.secure {
		return "__Host-onibi_device"
	}
	return "onibi_local_device"
}
func (s *Server) setDeviceCookie(w http.ResponseWriter, token string, expiry time.Time) {
	c := &http.Cookie{Name: s.deviceCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, Expires: expiry}
	if token == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}
func deviceFailure(w http.ResponseWriter) {
	writeError(w, 503, "device_unavailable", "Browser access could not be updated. Try again from your Mac.")
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request, session browserSession) {
	if err := s.RevokeDevice(r.Context(), session.Device.ID); err != nil {
		deviceFailure(w)
		return
	}
	s.setCookie(w, "", time.Unix(1, 0))
	s.setDeviceCookie(w, "", time.Unix(1, 0))
	writeJSON(w, 200, map[string]bool{"loggedOut": true})
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.ListDevices(r.Context())
	if err != nil {
		deviceFailure(w)
		return
	}
	writeJSON(w, 200, map[string]any{"devices": devices})
}
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request, session browserSession) {
	var body struct {
		ID string `json:"id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !validReceiptID(body.ID) {
		writeError(w, 400, "invalid_device", "Choose a valid device.")
		return
	}
	if err := s.RevokeDevice(r.Context(), body.ID); err != nil {
		deviceFailure(w)
		return
	}
	if body.ID == session.Device.ID {
		s.setCookie(w, "", time.Unix(1, 0))
		s.setDeviceCookie(w, "", time.Unix(1, 0))
	}
	writeJSON(w, 200, map[string]bool{"revoked": true})
}

// RevokeAllDevices is a local-only recovery operation. It also invalidates the
// outstanding pairing link; a fresh link must be explicitly requested afterward.
func (s *Server) RevokeAllDevices(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.SQL().ExecContext(ctx, `DELETE FROM web_devices WHERE origin=?`, s.origin); err != nil {
		return err
	}
	clear(s.sessions)
	s.log.Info("web.all_devices_revoked")
	s.pairUsed, s.pairURL = true, ""
	return nil
}
