package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
)

const uncertainMessage = "The action may have run. Inspect the terminal before sending it again."

var errReceiptConflict = errors.New("action ID already used for different input")

type Receipt struct {
	ID     string          `json:"id"`
	State  string          `json:"state"`
	Result *ghostty.Result `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (s *Server) prepareReceipts(ctx context.Context) error {
	_, err := s.db.SQL().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS web_action_receipts (
		id TEXT PRIMARY KEY, payload_hash TEXT NOT NULL, state TEXT NOT NULL,
		result_json BLOB, error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	// Keep the compact ID/hash tombstone indefinitely. Forgetting an old claim
	// would make an old retry executable again. Raw input is never persisted.
	_, err = s.db.SQL().ExecContext(ctx, `UPDATE web_action_receipts SET state='uncertain',error=?,updated_at=? WHERE state='pending'`, uncertainMessage, s.now().Unix())
	return err
}

func (s *Server) claimReceipt(ctx context.Context, id, hash string) (Receipt, bool, error) {
	now := s.now().Unix()
	result, err := s.db.SQL().ExecContext(ctx, `INSERT INTO web_action_receipts(id,payload_hash,state,created_at,updated_at) VALUES(?,?,'pending',?,?) ON CONFLICT(id) DO NOTHING`, id, hash, now, now)
	if err != nil {
		return Receipt{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Receipt{}, false, err
	}
	if changed == 1 {
		return Receipt{ID: id, State: "pending"}, true, nil
	}
	receipt, storedHash, err := s.receipt(ctx, id)
	if err != nil {
		return Receipt{}, false, err
	}
	if storedHash != hash {
		return Receipt{}, false, errReceiptConflict
	}
	return receipt, false, nil
}

func (s *Server) receipt(ctx context.Context, id string) (Receipt, string, error) {
	var receipt Receipt
	var raw []byte
	var hash string
	var createdAt int64
	err := s.db.SQL().QueryRowContext(ctx, `SELECT id,payload_hash,state,result_json,error,created_at FROM web_action_receipts WHERE id=?`, id).Scan(&receipt.ID, &hash, &receipt.State, &raw, &receipt.Error, &createdAt)
	if err != nil {
		return Receipt{}, "", err
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &receipt.Result); err != nil {
			return Receipt{}, "", err
		}
	}
	// A failed result write or interrupted claim must not leave the browser
	// polling forever. This is a read-only interpretation, not a retry or a
	// claim that the action stopped. A later durable result remains readable.
	if receipt.State == "pending" && !s.now().Before(time.Unix(createdAt, 0).Add(actionTimeout+10*time.Second)) {
		receipt.State, receipt.Error = "uncertain", uncertainMessage
	}
	return receipt, hash, nil
}

func (s *Server) finishAction(w http.ResponseWriter, receipt Receipt) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var raw []byte
	var err error
	if receipt.Result != nil {
		raw, err = json.Marshal(receipt.Result)
	}
	if err == nil {
		_, err = s.db.SQL().ExecContext(ctx, `UPDATE web_action_receipts SET state=?,result_json=?,error=?,updated_at=? WHERE id=? AND state='pending'`, receipt.State, raw, receipt.Error, s.now().Unix(), receipt.ID)
	}
	if err != nil {
		receipt.State, receipt.Result, receipt.Error = "uncertain", nil, uncertainMessage
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) actionStatus(w http.ResponseWriter, r *http.Request, id string) {
	if !validReceiptID(id) {
		writeError(w, http.StatusBadRequest, "invalid_action_id", "Invalid action ID.")
		return
	}
	receipt, _, err := s.receipt(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "action_not_found", "No receipt is available. Do not assume the action ran or resend it automatically.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "state_unavailable", "The action status is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}
