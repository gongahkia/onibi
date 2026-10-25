package devtool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceLauncherBannerPrecedesCompilerAndRunHandoff(t *testing.T) {
	for _, operation := range []string{"build", "run"} {
		for _, fail := range []bool{false, true} {
			t.Run(operation+map[bool]string{true: "-failure", false: "-success"}[fail], func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "source checkout")
				for _, dir := range []string{"scripts", "bin", "tools"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
				for _, name := range []string{"onibi-dev.sh", "onibi-banner.sh"} {
					raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "scripts", name), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				compiler := "#!/bin/sh\nprintf '[compiler] %s\\n' \"$*\"\n"
				if fail {
					compiler += "exit 23\n"
				}
				if err := os.WriteFile(filepath.Join(root, "tools", "go"), []byte(compiler), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "bin", "onibi"), []byte("#!/bin/sh\nprintf '[server handoff] %s\\n' \"$*\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(t.Context(), "/bin/sh", filepath.Join(root, "scripts", "onibi-dev.sh"), operation, "--verbose")
				cmd.Env = append(os.Environ(), "CI=1", "PATH="+filepath.Join(root, "tools")+":"+os.Getenv("PATH"))
				raw, err := cmd.CombinedOutput()
				output := string(raw)
				banner := "Onibi / Control Ghostty wherever text renders"
				if !strings.HasPrefix(output, banner+"\n") || strings.Count(output, banner) != 1 {
					t.Fatalf("banner was not first or repeated:\n%s", output)
				}
				if strings.Index(output, "[prepare]") > strings.Index(output, "[compiler]") {
					t.Fatal("compiler ran before preparation output")
				}
				if !strings.Contains(output, "[compiler] run ./cmd/onibi-dev build --verbose") {
					t.Fatalf("wrong build handoff:\n%s", output)
				}
				if fail {
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 23 {
						t.Fatalf("failure status lost: %v", err)
					}
					if strings.Contains(output, "[server handoff]") {
						t.Fatal("server launched after failed build")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if got := strings.Contains(output, "[server handoff] web --no-banner"); got != (operation == "run") {
						t.Fatalf("wrong server handoff:\n%s", output)
					}
				}
			})
		}
	}
}
