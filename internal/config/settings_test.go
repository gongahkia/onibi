package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMapsLegacyOutputBufferAndSaveWritesCurrentKey(t *testing.T) {
	paths := Paths{StateDir: t.TempDir(), Config: filepath.Join(t.TempDir(), "config.yaml")}
	cfg, meta, err := loadBytes(paths.Config, []byte("daemon:\n  pty_buffer_bytes: 8192\nshell:\n  default: bash\n  login: false\n"), Default(), LoadMeta{Path: paths.Config, Explicit: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.OutputBufferBytes != 8192 || !meta.Explicit["daemon.output_buffer_bytes"] || cfg.Shell.Default != "bash" || cfg.Shell.Login {
		t.Fatalf("config=%#v meta=%#v", cfg, meta)
	}
	if err := Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(paths)
	if err != nil || loaded.Daemon.OutputBufferBytes != 8192 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestSetRejectsRemovedPTYKey(t *testing.T) {
	if err := Set(&Config{}, "daemon.pty_buffer_bytes", "8192"); err == nil {
		t.Fatal("accepted removed key")
	}
}
