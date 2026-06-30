package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/snapshot"
	"github.com/gongahkia/onibi/internal/store"
)

func (d *Daemon) handleSnapshotRPC(ctx context.Context, ev intake.Event) (intake.Response, error) {
	if d == nil || d.DB == nil {
		return intake.Response{}, errors.New("snapshot store unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(ev.SnapshotAction)) {
	case "take":
		return d.handleSnapshotTake(ctx, ev)
	case "list":
		return d.handleSnapshotList(ctx)
	case "delete":
		return d.handleSnapshotDelete(ctx, ev)
	case "restore":
		return d.handleSnapshotRestore(ctx, ev)
	case "fork":
		return d.handleSnapshotFork(ctx, ev)
	default:
		return intake.Response{}, errors.New("bad snapshot action")
	}
}

func (d *Daemon) handleSnapshotTake(ctx context.Context, ev intake.Event) (intake.Response, error) {
	name := strings.TrimSpace(ev.SnapshotName)
	if name == "" {
		return intake.Response{}, errors.New("snapshot name required")
	}
	s, err := d.sessionForRPCTarget(ev.Session)
	if err != nil {
		return intake.Response{}, err
	}
	snap, err := snapshot.TakeContext(ctx, s, snapshot.Options{})
	if err != nil {
		return intake.Response{}, err
	}
	id := NewID()
	if err := d.DB.SnapshotSave(ctx, store.SnapshotEntry{
		ID:               id,
		SessionID:        snap.SessionID,
		Name:             name,
		CreatedAt:        snap.CreatedAt,
		RingBuffer:       snap.RingBuffer,
		CWD:              snap.CWD,
		Env:              snap.Env,
		TranscriptOffset: snap.TranscriptOffset,
	}); err != nil {
		return intake.Response{}, err
	}
	d.audit(ctx, "snapshot.take", s.ID, "", 0, "name="+name+" id="+id)
	return intake.Response{SessionID: s.ID, Text: "snapshot " + name + " saved"}, nil
}

func (d *Daemon) handleSnapshotList(ctx context.Context) (intake.Response, error) {
	rows, err := d.DB.SnapshotsList(ctx)
	if err != nil {
		return intake.Response{}, err
	}
	if len(rows) == 0 {
		return intake.Response{Text: "no snapshots"}, nil
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", row.Name, row.SessionID, row.CreatedAt.Format(time.RFC3339), row.CWD)
	}
	return intake.Response{Text: strings.TrimRight(b.String(), "\n")}, nil
}

func (d *Daemon) handleSnapshotDelete(ctx context.Context, ev intake.Event) (intake.Response, error) {
	name := strings.TrimSpace(ev.SnapshotName)
	if name == "" {
		return intake.Response{}, errors.New("snapshot name required")
	}
	ok, err := d.DB.SnapshotDeleteByName(ctx, name)
	if err != nil {
		return intake.Response{}, err
	}
	if !ok {
		return intake.Response{}, errors.New("snapshot not found")
	}
	d.audit(ctx, "snapshot.delete", "", "", 0, "name="+name)
	return intake.Response{Text: "snapshot " + name + " deleted"}, nil
}

func (d *Daemon) handleSnapshotRestore(ctx context.Context, ev intake.Event) (intake.Response, error) {
	snap, entry, err := d.loadSnapshot(ctx, ev.SnapshotName)
	if err != nil {
		return intake.Response{}, err
	}
	restored, err := snapshot.RestoreContext(ctx, snap, snapshot.RestoreOptions{})
	if err != nil {
		return intake.Response{}, err
	}
	s, err := d.registerSnapshotSession(ctx, entry.Name, restored)
	if err != nil {
		return intake.Response{}, err
	}
	d.audit(ctx, "snapshot.restore", s.ID, "", 0, "name="+entry.Name)
	return intake.Response{SessionID: s.ID, Text: "restored " + entry.Name + " as " + s.ID}, nil
}

func (d *Daemon) handleSnapshotFork(ctx context.Context, ev intake.Event) (intake.Response, error) {
	snap, entry, err := d.loadSnapshot(ctx, ev.SnapshotName)
	if err != nil {
		return intake.Response{}, err
	}
	restored, err := snapshot.ForkContext(ctx, snap, ev.SnapshotTurn, ev.Text, snapshot.RestoreOptions{})
	if err != nil {
		return intake.Response{}, err
	}
	s, err := d.registerSnapshotSession(ctx, entry.Name+"-fork", restored)
	if err != nil {
		return intake.Response{}, err
	}
	d.audit(ctx, "snapshot.fork", s.ID, "", 0, fmt.Sprintf("name=%s turn=%d", entry.Name, ev.SnapshotTurn))
	return intake.Response{SessionID: s.ID, Text: "forked " + entry.Name + " as " + s.ID}, nil
}

func (d *Daemon) loadSnapshot(ctx context.Context, name string) (snapshot.Snapshot, store.SnapshotEntry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return snapshot.Snapshot{}, store.SnapshotEntry{}, errors.New("snapshot name required")
	}
	entry, ok, err := d.DB.SnapshotByName(ctx, name)
	if err != nil {
		return snapshot.Snapshot{}, store.SnapshotEntry{}, err
	}
	if !ok {
		return snapshot.Snapshot{}, store.SnapshotEntry{}, errors.New("snapshot not found")
	}
	session, ok, err := d.DB.SessionByID(ctx, entry.SessionID)
	if err != nil {
		return snapshot.Snapshot{}, store.SnapshotEntry{}, err
	}
	if !ok {
		return snapshot.Snapshot{}, store.SnapshotEntry{}, errors.New("snapshot source session not found")
	}
	snap := snapshot.Snapshot{
		SessionID:        entry.SessionID,
		SessionName:      session.Name,
		Agent:            session.Agent,
		Command:          session.Command,
		CreatedAt:        entry.CreatedAt,
		RingBuffer:       entry.RingBuffer,
		CWD:              entry.CWD,
		Env:              entry.Env,
		TranscriptOffset: entry.TranscriptOffset,
	}
	if strings.EqualFold(snap.Agent, "claude") {
		snap.Transcript = d.findClaudeTranscript(snap.SessionID, snap.CWD)
	}
	return snap, entry, nil
}

func (d *Daemon) findClaudeTranscript(sessionID, cwd string) string {
	parser := d.Budget
	if parser == nil {
		parser = budget.NewClaudeParser("")
	}
	path, err := parser.FindTranscript(budget.SessionRef{SessionID: sessionID, Agent: "claude", CWD: cwd})
	if err != nil {
		return ""
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func (d *Daemon) registerSnapshotSession(ctx context.Context, name string, restored snapshot.Session) (*Session, error) {
	id := NewID()
	agent := strings.TrimSpace(restored.Agent)
	if agent == "" {
		agent = "shell"
	}
	if strings.TrimSpace(name) == "" {
		name = agent
	}
	s := NewSession(id, name, agent, restored.Host, d.bufferSize())
	s.CWD = restored.CWD
	s.Cmd = restored.Command
	if err := d.Registry.Add(s); err != nil {
		_ = restored.Host.Close()
		return nil, err
	}
	d.persistSessionStart(ctx, s, s.CWD)
	go d.readLoop(s)
	go d.waitHost(s)
	return s, nil
}
