package editor

import (
	"encoding/hex"
	"fmt"
	"github.com/gongahkia/shack/internal/recovery"
	"github.com/gongahkia/shack/internal/text"
	"strings"
	"time"
)

func (a *App) startRecovery() {
	if !a.Config.Recovery || a.Readonly {
		return
	}
	store, err := recovery.Open(recovery.Directory(), a.Config.RecoveryMaxBytes)
	if err != nil {
		a.Status = "Recovery unavailable: " + err.Error()
		return
	}
	a.recovery = store
	entries, err := store.Scan()
	if err == nil {
		a.recoverable = entries
	}
	if len(entries) > 0 {
		a.Status = "Unsaved work found · use Recover unsaved buffers in commands"
		if len(a.Buffers) == 1 && a.Current().File.Path == "" && a.Current().Doc.Text.Len() == 0 {
			a.recoveryPicker()
		}
	}
}
func (a *App) recoveryRecord() (recovery.Record, error) {
	record := recovery.Record{}
	total := 0
	for _, b := range a.Buffers {
		if !b.Doc.Dirty() || b.Transient {
			continue
		}
		total += b.Doc.Text.Len()
		if total > a.Config.RecoveryMaxBytes {
			return record, fmt.Errorf("unsaved text exceeds recovery limit (%d MiB)", a.Config.RecoveryMaxBytes>>20)
		}
		record.Buffers = append(record.Buffers, recovery.Buffer{Path: b.File.Path, Contents: b.Doc.Text.String(), Selections: append([]text.Selection(nil), b.Doc.Selections...), BaseHash: hex.EncodeToString(b.File.hash[:]), Existed: b.File.exists})
	}
	return record, nil
}
func (a *App) scheduleRecovery() {
	a.recoveryDirty = true
	if a.recovery == nil || a.recoveryC != nil {
		return
	}
	a.recoveryTimer = time.NewTimer(2 * time.Second)
	a.recoveryC = a.recoveryTimer.C
}
func (a *App) writeRecovery(sync bool) error {
	if a.recovery == nil {
		return nil
	}
	if !sync && a.recoveryBusy {
		return nil
	}
	record, err := a.recoveryRecord()
	if err != nil {
		a.Status = "Recovery: " + err.Error()
		return err
	}
	a.recoverySeq++
	sequence := a.recoverySeq
	store := a.recovery
	if sync {
		return store.Save(sequence, record)
	}
	a.recoveryBusy = true
	a.recoveryDirty = false
	go func() {
		err := store.Save(sequence, record)
		a.post(func() {
			a.recoveryBusy = false
			if err != nil {
				a.Status = "Recovery: " + err.Error()
			}
			if a.recoveryDirty {
				a.scheduleRecovery()
			}
		})
	}()
	return nil
}
func (a *App) finishRecovery() {
	if a.recovery == nil {
		return
	}
	if !a.quit {
		_ = a.writeRecovery(true)
	}
	a.recovery.Close(a.quit)
	for _, entry := range a.recoverable {
		_ = entry.Release(false)
	}
}
func (a *App) recoveryPicker() {
	if len(a.recoverable) == 0 {
		a.Status = "No abandoned recovery snapshots"
		return
	}
	items := []choice{}
	for _, entry := range a.recoverable {
		entry := entry
		items = append(items, choice{entry.Label(), func() {
			a.list("Recover unsaved work", []choice{{"Restore buffers (original files remain untouched)", func() { a.restoreRecovery(entry) }}, {"Discard this recovery snapshot", func() {
				if err := entry.Release(true); err != nil {
					a.Status = err.Error()
				}
				a.removeRecoveryEntry(entry)
			}}})
		}})
	}
	a.list("Recovery snapshots", items)
}
func (a *App) removeRecoveryEntry(entry *recovery.Entry) {
	for i, e := range a.recoverable {
		if e == entry {
			a.recoverable = append(a.recoverable[:i], a.recoverable[i+1:]...)
			break
		}
	}
}
func (a *App) restoreRecovery(entry *recovery.Entry) {
	staged := []*Buffer{}
	for _, saved := range entry.Record.Buffers {
		var file *File
		var d *text.Document
		var err error
		if saved.Path != "" {
			file, d, err = OpenFile(saved.Path, int64(a.Config.MaxFileMiB)<<20)
		} else {
			file = &File{}
			d, _ = text.NewDocument("")
		}
		if err != nil {
			a.Status = err.Error()
			return
		}
		// An already-open buffer is kept; a recovered conflict becomes an untitled copy.
		for _, b := range a.Buffers {
			if saved.Path != "" && b.File.Path == saved.Path {
				file = &File{}
				d, _ = text.NewDocument("")
				break
			}
		}
		if err = d.Apply(d.Version, []text.Edit{{Start: 0, End: d.Text.Len(), Text: saved.Contents}}); err != nil {
			a.Status = err.Error()
			return
		}
		if file.Path != "" {
			hash, e := hex.DecodeString(saved.BaseHash)
			if e != nil || len(hash) != 32 {
				a.Status = "Invalid recovery fingerprint"
				return
			}
			copy(file.hash[:], hash)
			file.exists = saved.Existed
		}
		for _, s := range saved.Selections {
			if !d.Text.Boundary(s.Anchor) || !d.Text.Boundary(s.Head) {
				a.Status = "Invalid recovery selection"
				return
			}
		}
		if len(saved.Selections) > 0 {
			d.Selections = saved.Selections
		}
		staged = append(staged, &Buffer{File: file, Doc: d, Newline: detectNewline(d.Text)})
	}
	a.Buffers = append(a.Buffers, staged...)
	a.Active = len(a.Buffers) - 1
	if err := a.writeRecovery(true); err == nil {
		_ = entry.Release(true)
		a.removeRecoveryEntry(entry)
	}
	a.Status = "Recovered unsaved text; review before saving"
	a.schedule()
	a.reveal()
}
func detectNewline(r text.Rope) string {
	if strings.Contains(r.Slice(0, min(r.Len(), 8192)), "\r\n") {
		return "\r\n"
	}
	return "\n"
}
