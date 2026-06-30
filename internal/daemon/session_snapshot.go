package daemon

import "github.com/gongahkia/onibi/internal/pty"

func (s *Session) SnapshotID() string {
	if s == nil {
		return ""
	}
	return s.ID
}

func (s *Session) SnapshotName() string {
	if s == nil {
		return ""
	}
	return s.Name
}

func (s *Session) SnapshotAgent() string {
	if s == nil {
		return ""
	}
	return s.Agent
}

func (s *Session) SnapshotCommand() string {
	if s == nil {
		return ""
	}
	return s.Cmd
}

func (s *Session) SnapshotCWD() string {
	if s == nil {
		return ""
	}
	return s.CWD
}

func (s *Session) SnapshotPID() int {
	if s == nil || s.Host == nil || s.Host.Cmd == nil || s.Host.Cmd.Process == nil {
		return 0
	}
	return s.Host.Cmd.Process.Pid
}

func (s *Session) SnapshotHost() *pty.Host {
	if s == nil {
		return nil
	}
	return s.Host
}

func (s *Session) SnapshotBuffer() []byte {
	if s == nil || s.Buf == nil {
		return nil
	}
	return s.Buf.Snapshot()
}
