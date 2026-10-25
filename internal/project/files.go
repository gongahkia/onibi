// Package project provides bounded project discovery without mandatory tools.
package project

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const MaxFiles = 10000

// Files uses Git's complete ignore semantics when Git is available in a repository.
// The standalone fallback skips common dependency/build directories and symlinks.
func Files(ctx context.Context, root string) (files []string, mode string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-c", "-o", "--exclude-standard", "-z", "--", ".")
	cmd.WaitDelay = time.Second
	out, e := cmd.StdoutPipe()
	if e == nil {
		e = cmd.Start()
	}
	if e == nil {
		reader := bufio.NewReaderSize(out, 4096)
		seen := map[string]bool{}
		limited := false
		for {
			p, e := reader.ReadString(0)
			if e != nil {
				break
			}
			p = strings.TrimSuffix(p, "\x00")
			if len(p) > 4096 {
				continue
			}
			if !seen[p] && safeFile(root, p) {
				files = append(files, p)
				seen[p] = true
			}
			if len(files) >= MaxFiles {
				limited = true
				_ = cmd.Process.Kill()
				break
			}
		}
		e = cmd.Wait()
		if e == nil || limited {
			sort.Strings(files)
			mode = "Git ignore rules"
			if limited {
				mode += "; first 10,000 files"
			}
			return files, mode, nil
		}
	}
	files = nil
	mode = "built-in directory exclusions (Git unavailable or no repository)"
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			return nil
		}
		if len(files) >= MaxFiles {
			mode += "; first 10,000 files"
			return fs.SkipAll
		}
		if entry.IsDir() {
			if path != root {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", ".cache", "_build", "_opam", ".venv", "target", "build", "dist":
					return fs.SkipDir
				}
			}
			return nil
		}
		if entry.Type().IsRegular() {
			rel, e := filepath.Rel(root, path)
			if e == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	sort.Strings(files)
	return
}
func safeFile(root, p string) bool {
	if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(filepath.Clean(p), ".."+string(filepath.Separator)) {
		return false
	}
	st, err := os.Lstat(filepath.Join(root, p))
	return err == nil && st.Mode().IsRegular()
}

// Inside resolves parent symlinks before checking the project boundary.
func Inside(root, path string) bool {
	root, _ = filepath.Abs(root)
	if p, e := filepath.EvalSymlinks(root); e == nil {
		root = p
	}
	path, _ = filepath.Abs(path)
	if p, e := filepath.EvalSymlinks(path); e == nil {
		path = p
	}
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
