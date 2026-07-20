package denytest

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type HookResult struct {
	Stdout string
	Stderr string
	Code   int
}

func Target(t testing.TB, adapter string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "onibi-deny-"+adapter+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".txt")
}

func AssertNotCreated(t testing.TB, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("deny target was created: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat deny target: %v", err)
	}
}

func CreateIfAllowed(t testing.TB, path string, allowed bool) {
	t.Helper()
	if allowed {
		if err := os.WriteFile(path, []byte("created\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	AssertNotCreated(t, path)
}

func DenyNotify(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onibi-notify")
	body := `#!/bin/sh
case " $* " in
  *" --response onibi-json "*) printf '{"decision":"deny","reason":"deny fixture"}\n'; exit 0 ;;
  *" --format gemini "*) printf '{"decision":"deny","reason":"deny fixture"}\n'; exit 0 ;;
  *" --format copilot "*) printf '{"permissionDecision":"deny","permissionDecisionReason":"deny fixture"}\n'; exit 0 ;;
  *" --format goose "*) printf '{"decision":"block","reason":"deny fixture"}\n'; exit 0 ;;
esac
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"deny fixture"}}\n'
printf 'deny fixture\n' >&2
exit 2
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func RunHook(t testing.TB, command, payload string) HookResult {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=deny-test")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return HookResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

func Node(t testing.TB) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	return path
}

func TSC(t testing.TB) string {
	t.Helper()
	path, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	return path
}

func CompileTSModule(t testing.TB, tsc, dir, filename, source, types string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, filename)
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	typesPath := filepath.Join(dir, "types.d.ts")
	if err := os.WriteFile(typesPath, []byte(types), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	cmd := exec.Command(tsc, "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--skipLibCheck", "--outDir", outDir, src, typesPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc failed: %v\n%s", err, out)
	}
	return filepath.Join(outDir, strings.TrimSuffix(filename, filepath.Ext(filename))+".js")
}

func RunNodeScript(t testing.TB, node, dir, script string, args ...string) string {
	t.Helper()
	path := filepath.Join(dir, "run.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ONIBI_SESSION_ID=deny-test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node fixture failed: %v\n%s", err, out)
	}
	return string(out)
}
