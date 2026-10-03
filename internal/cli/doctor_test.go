package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

func TestDoctorReportFailures(t *testing.T) {
	report := doctorReport{}
	report.add(doctorOK, "tmux", "available")
	report.add(doctorInfo, "codex", "optional")
	report.add(doctorFail, "token", "missing")
	if got, want := report.failures(), 1; got != want {
		t.Fatalf("failures() = %d, want %d", got, want)
	}
}

func TestDoctorCheckState(t *testing.T) {
	t.Run("missing state is ready for first setup", func(t *testing.T) {
		base := t.TempDir()
		paths := config.Paths{StateDir: filepath.Join(base, "onibi"), EnvFile: filepath.Join(base, "onibi", ".env")}
		report := doctorReport{}
		doctorCheckState(&report, paths)
		if len(report.checks) != 1 || report.checks[0].Level != doctorOK {
			t.Fatalf("checks = %#v, want one OK check", report.checks)
		}
	})

	t.Run("broad state permissions fail", func(t *testing.T) {
		state := filepath.Join(t.TempDir(), "onibi")
		if err := os.Mkdir(state, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(state, 0o755); err != nil {
			t.Fatal(err)
		}
		paths := config.Paths{StateDir: state, EnvFile: filepath.Join(state, ".env")}
		report := doctorReport{}
		doctorCheckState(&report, paths)
		if len(report.checks) != 1 || report.checks[0].Level != doctorFail {
			t.Fatalf("checks = %#v, want one FAIL check", report.checks)
		}
	})
}
