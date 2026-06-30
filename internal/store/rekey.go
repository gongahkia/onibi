package store

import (
	"context"
	"errors"
)

func (d *DB) Rekey(ctx context.Context, newMasterKey []byte) error {
	if d == nil || d.cryptbox == nil {
		return ErrCryptBoxUnavailable
	}
	newBox, err := NewCryptBox(newMasterKey)
	if err != nil {
		return err
	}
	pairings, err := d.rekeyPairingRows(ctx, newBox)
	if err != nil {
		return err
	}
	sessions, err := d.rekeyWebSessionRows(ctx, newBox)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range pairings {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pairing_tokens SET token_enc = ? WHERE token_hash = ?`,
			r.tokenEnc, r.hash); err != nil {
			return err
		}
	}
	for _, r := range sessions {
		if _, err := tx.ExecContext(ctx,
			`UPDATE web_sessions SET cookie_enc = ?, user_agent_enc = ?, key_verifier_enc = ? WHERE cookie_hash = ?`,
			r.cookieEnc, r.userAgentEnc, r.keyVerifierEnc, r.hash); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.cryptbox = newBox
	return nil
}

func (d *DB) VerifyEncryptedState(ctx context.Context) error {
	if d == nil || d.cryptbox == nil {
		return ErrCryptBoxUnavailable
	}
	if _, err := d.rekeyPairingRows(ctx, d.cryptbox); err != nil {
		return err
	}
	if _, err := d.rekeyWebSessionRows(ctx, d.cryptbox); err != nil {
		return err
	}
	return nil
}

type rekeyPairingRow struct {
	hash     string
	tokenEnc []byte
}

type rekeyWebSessionRow struct {
	hash           string
	cookieEnc      []byte
	userAgentEnc   []byte
	keyVerifierEnc []byte
}

func (d *DB) rekeyPairingRows(ctx context.Context, newBox *CryptBox) ([]rekeyPairingRow, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT token_hash, token_enc FROM pairing_tokens`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rekeyPairingRow
	for rows.Next() {
		var hash string
		var sealed []byte
		if err := rows.Scan(&hash, &sealed); err != nil {
			return nil, err
		}
		plain, err := d.openString(ctx, "pairing_tokens", hash, "token_enc", sealed)
		if err != nil {
			return nil, err
		}
		next, err := newBox.Seal(ctx, []byte(plain), RowAAD("pairing_tokens", hash, "token_enc"))
		if err != nil {
			return nil, err
		}
		out = append(out, rekeyPairingRow{hash: hash, tokenEnc: next})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DB) rekeyWebSessionRows(ctx context.Context, newBox *CryptBox) ([]rekeyWebSessionRow, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT cookie_hash, cookie_enc, user_agent_enc, key_verifier_enc FROM web_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rekeyWebSessionRow
	for rows.Next() {
		var hash string
		var cookieEnc, userAgentEnc, keyVerifierEnc []byte
		if err := rows.Scan(&hash, &cookieEnc, &userAgentEnc, &keyVerifierEnc); err != nil {
			return nil, err
		}
		cookie, err := d.openString(ctx, "web_sessions", hash, "cookie_enc", cookieEnc)
		if err != nil {
			return nil, err
		}
		userAgent, err := d.openString(ctx, "web_sessions", hash, "user_agent_enc", userAgentEnc)
		if err != nil {
			return nil, err
		}
		nextCookie, err := newBox.Seal(ctx, []byte(cookie), RowAAD("web_sessions", hash, "cookie_enc"))
		if err != nil {
			return nil, err
		}
		nextUserAgent, err := newBox.Seal(ctx, []byte(userAgent), RowAAD("web_sessions", hash, "user_agent_enc"))
		if err != nil {
			return nil, err
		}
		var nextVerifier []byte
		if len(keyVerifierEnc) > 0 {
			verifier, err := d.openBytes(ctx, "web_sessions", hash, "key_verifier_enc", keyVerifierEnc)
			if err != nil {
				return nil, err
			}
			nextVerifier, err = newBox.Seal(ctx, verifier, RowAAD("web_sessions", hash, "key_verifier_enc"))
			if err != nil {
				return nil, err
			}
		}
		out = append(out, rekeyWebSessionRow{hash: hash, cookieEnc: nextCookie, userAgentEnc: nextUserAgent, keyVerifierEnc: nextVerifier})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func IsCryptBoxUnavailable(err error) bool {
	return errors.Is(err, ErrCryptBoxUnavailable)
}
