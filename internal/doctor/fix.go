package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
)

type FixReport struct {
	Actions []string
	Errors  []error
}

func (r FixReport) Failed() bool { return len(r.Errors) > 0 }

func Fix(ctx context.Context, opts Options) FixReport {
	f := fixer{ctx: ctx, opts: opts}
	f.run()
	return FixReport{Actions: f.actions, Errors: f.errs}
}

type fixer struct {
	ctx     context.Context
	opts    Options
	actions []string
	errs    []error
}

func (f *fixer) add(action string) {
	f.actions = append(f.actions, action)
}

func (f *fixer) err(action string, err error) {
	if err != nil {
		f.errs = append(f.errs, fmt.Errorf("%s: %w", action, err))
	}
}

func (f *fixer) run() {
	if err := f.opts.Paths.EnsureDirs(); err != nil {
		f.err("ensure state dirs", err)
	} else {
		f.add("ensured state dirs")
	}
	f.fixPerms()
	f.fixSocket()
	f.fixHookHashes()
}

func (f *fixer) fixPerms() {
	if err := os.Chmod(f.opts.Paths.StateDir, 0o700); err == nil {
		f.add("chmod state dir 0700")
	} else if !os.IsNotExist(err) {
		f.err("chmod state dir", err)
	}
	if _, err := os.Stat(f.opts.Paths.EnvFile); err == nil {
		if err := os.Chmod(f.opts.Paths.EnvFile, 0o600); err != nil {
			f.err("chmod env file", err)
		} else {
			f.add("chmod .env 0600")
		}
	} else if !os.IsNotExist(err) {
		f.err("stat env file", err)
	}
	if _, err := os.Stat(f.opts.Paths.DBFile); err == nil {
		if err := os.Chmod(f.opts.Paths.DBFile, 0o600); err != nil {
			f.err("chmod sqlite db", err)
		} else {
			f.add("chmod sqlite db 0600")
		}
	}
}

func (f *fixer) fixSocket() {
	fi, err := os.Stat(f.opts.Paths.Socket)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		f.err("stat socket", err)
		return
	}
	if fi.Mode().Perm() != 0o600 {
		if err := os.Chmod(f.opts.Paths.Socket, 0o600); err != nil {
			f.err("chmod socket", err)
		} else {
			f.add("chmod socket 0600")
		}
	}
	c, err := net.DialTimeout("unix", f.opts.Paths.Socket, 300*time.Millisecond)
	if err == nil {
		_ = c.Close()
		return
	}
	if err := os.Remove(f.opts.Paths.Socket); err != nil && !os.IsNotExist(err) {
		f.err("remove stale socket", err)
	} else {
		f.add("removed stale socket")
	}
}

func (f *fixer) fixHookHashes() {
	db, err := openStoreDB(f.opts.Paths.DBFile)
	if err != nil {
		return
	}
	defer db.Close()
	notifyBin := f.opts.NotifyBin
	notify := func() (string, bool) {
		if notifyBin != "" {
			return notifyBin, true
		}
		var err error
		notifyBin, err = locateNotifyBinary()
		if err != nil {
			f.err("locate onibi-notify", err)
			return "", false
		}
		return notifyBin, true
	}
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(f.ctx, db)
		if info.Tampered {
			f.err("refuse "+name+" hook", fmt.Errorf("managed hook tampered; uninstall and reinstall manually"))
			continue
		}
		if info.Outdated && info.Installed && info.Managed && info.HashRecorded {
			bin, ok := notify()
			if !ok {
				continue
			}
			if err := a.Install(f.ctx, db, bin); err != nil {
				f.err("reinstall "+name+" hook", err)
			} else {
				f.add("reinstalled " + name + " hook")
			}
			continue
		}
		if info.Adoptable && !info.Tampered && a.Adopt != nil {
			if err := a.Adopt(f.ctx, db); err != nil {
				f.err("adopt "+name+" hook", err)
			} else {
				f.add("adopted " + name + " hook hash")
			}
		}
	}
}

func locateNotifyBinary() (string, error) {
	if env := strings.TrimSpace(os.Getenv("ONIBI_NOTIFY_BIN")); env != "" {
		if filepath.IsAbs(env) {
			return env, nil
		}
		return filepath.Abs(env)
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "onibi-notify")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath("onibi-notify"); err == nil {
		return filepath.Abs(p)
	}
	return "", fmt.Errorf("onibi-notify binary not found")
}
