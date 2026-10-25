// Package fileops stages bounded workspace resource changes before touching disk.
// Commit detects conflicting disk changes and attempts rollback on I/O failure.
package fileops

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const MaxBytes = 8 << 20
const MaxNodes = 512

type Kind uint8

const (
	Missing Kind = iota
	File
	Directory
)

type Node struct {
	Kind     Kind
	Data     string
	Mode     fs.FileMode
	UID, GID int
	// Listed directories detect additions/removals before deleting/moving a tree.
	Listed   bool
	Children []string
}
type Plan struct {
	Root          string
	Before, After map[string]Node
	bytes         int
}

func New(root string) (*Plan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("workspace root is not a directory")
	}
	return &Plan{Root: root, Before: map[string]Node{}, After: map[string]Node{}}, nil
}

func Inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func checkedPath(root, path string) (string, error) {
	for at := root; ; at = filepath.Dir(at) {
		info, err := os.Lstat(at)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("workspace root changed or crosses a symlink")
		}
		if at == filepath.Dir(at) {
			break
		}
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("workspace operation needs an absolute file path")
	}
	path = filepath.Clean(path)
	if path == root || !Inside(root, path) {
		return "", errors.New("workspace operation must stay below the project root")
	}
	for at := path; at != root; at = filepath.Dir(at) {
		info, err := os.Lstat(at)
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("resource operation crosses a symlink: %s", at)
		}
	}
	return path, nil
}

func diskNode(path string, remaining int) (Node, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return Node{}, nil
	}
	if err != nil {
		return Node{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Node{}, errors.New("file ownership metadata is unavailable")
	}
	n := Node{Mode: info.Mode().Perm(), UID: int(stat.Uid), GID: int(stat.Gid)}
	if info.IsDir() {
		n.Kind = Directory
		return n, nil
	}
	if !info.Mode().IsRegular() {
		return Node{}, fmt.Errorf("resource operation needs regular files/directories: %s", path)
	}
	if stat.Nlink > 1 {
		return Node{}, fmt.Errorf("resource operation cannot replace a hard-linked file: %s", path)
	}
	if info.Size() > int64(remaining) {
		return Node{}, errors.New("workspace resource snapshot exceeds 8 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return Node{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(remaining)+1))
	if err != nil {
		return Node{}, err
	}
	if len(data) > remaining {
		return Node{}, errors.New("workspace resource snapshot exceeds 8 MiB")
	}
	n.Kind, n.Data = File, string(data)
	return n, nil
}

func (p *Plan) Path(path string) (string, error) { return checkedPath(p.Root, path) }
func (p *Plan) Read(path string) (Node, error) {
	path, err := p.Path(path)
	if err != nil {
		return Node{}, err
	}
	if n, ok := p.After[path]; ok {
		return n, nil
	}
	if len(p.Before) >= MaxNodes {
		return Node{}, errors.New("workspace resource snapshot exceeds 512 paths")
	}
	n, err := diskNode(path, MaxBytes-p.bytes)
	if err != nil {
		return Node{}, err
	}
	p.bytes += len(n.Data)
	p.Before[path] = n
	// A previously deleted/replaced ancestor masks its old on-disk descendants.
	for at := filepath.Dir(path); at != p.Root; at = filepath.Dir(at) {
		if parent, ok := p.After[at]; ok && (parent.Kind != Directory || p.Before[at].Kind != Directory) {
			n = Node{}
			break
		}
	}
	p.After[path] = n
	return n, nil
}
func (p *Plan) parents(path string) error {
	parent := filepath.Dir(path)
	if parent == p.Root {
		return nil
	}
	n, err := p.Read(parent)
	if err != nil {
		return err
	}
	if n.Kind == File {
		return fmt.Errorf("parent is a file: %s", parent)
	}
	if n.Kind == Directory {
		return nil
	}
	if err = p.parents(parent); err != nil {
		return err
	}
	p.After[parent] = Node{Kind: Directory, Mode: 0755, UID: os.Getuid(), GID: os.Getgid()}
	return nil
}

// tree loads all existing descendants and records direct directory membership.
// It never follows links, and both bytes and number of paths are bounded.
func (p *Plan) tree(path string) ([]string, error) {
	n, err := p.Read(path)
	if err != nil {
		return nil, err
	}
	if n.Kind == Missing {
		return nil, fmt.Errorf("resource is missing: %s", path)
	}
	if n.Kind == File {
		return []string{path}, nil
	}
	before := p.Before[path]
	if before.Kind == Directory && !before.Listed {
		dir, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		names, err := dir.Readdirnames(MaxNodes + 1)
		dir.Close()
		if err != nil && err != io.EOF {
			return nil, err
		}
		if len(names) > MaxNodes {
			return nil, errors.New("resource directory exceeds 512 paths")
		}
		slices.Sort(names)
		before.Children, before.Listed = names, true
		p.Before[path] = before
		for _, name := range names {
			if _, err = p.Read(filepath.Join(path, name)); err != nil {
				return nil, err
			}
		}
	}
	children := []string{}
	for candidate, node := range p.After {
		if filepath.Dir(candidate) == path && node.Kind != Missing {
			children = append(children, candidate)
		}
	}
	slices.Sort(children)
	out := []string{path}
	for _, child := range children {
		paths, err := p.tree(child)
		if err != nil {
			return nil, err
		}
		out = append(out, paths...)
	}
	return out, nil
}

func (p *Plan) Create(path string, overwrite, ignore bool) (bool, error) {
	path, err := p.Path(path)
	if err != nil {
		return false, err
	}
	n, err := p.Read(path)
	if err != nil {
		return false, err
	}
	if n.Kind != Missing {
		if !overwrite && ignore {
			return false, nil
		}
		if !overwrite {
			return false, fmt.Errorf("create destination exists: %s", path)
		}
		if n.Kind != File {
			return false, errors.New("cannot overwrite a directory with CreateFile")
		}
	} else {
		n = Node{Kind: File, Mode: 0644, UID: os.Getuid(), GID: os.Getgid()}
	}
	if err = p.parents(path); err != nil {
		return false, err
	}
	n.Data = ""
	p.After[path] = n
	return true, nil
}

func (p *Plan) Delete(path string, recursive, ignore bool) (bool, error) {
	path, err := p.Path(path)
	if err != nil {
		return false, err
	}
	n, err := p.Read(path)
	if err != nil {
		return false, err
	}
	if n.Kind == Missing {
		if ignore {
			return false, nil
		}
		return false, fmt.Errorf("delete target is missing: %s", path)
	}
	paths, err := p.tree(path)
	if err != nil {
		return false, err
	}
	if n.Kind == Directory && !recursive && len(paths) > 1 {
		return false, errors.New("deleting a nonempty directory requires recursive=true")
	}
	for _, at := range paths {
		p.After[at] = Node{}
	}
	return true, nil
}

func (p *Plan) Rename(from, to string, overwrite, ignore bool) (bool, error) {
	from, err := p.Path(from)
	if err != nil {
		return false, err
	}
	to, err = p.Path(to)
	if err != nil {
		return false, err
	}
	if from == to {
		return false, nil
	}
	if Inside(from, to) || Inside(to, from) {
		return false, errors.New("cannot rename a resource into itself or its parent")
	}
	source, err := p.Read(from)
	if err != nil {
		return false, err
	}
	if source.Kind == Missing {
		return false, fmt.Errorf("rename source is missing: %s", from)
	}
	destination, err := p.Read(to)
	if err != nil {
		return false, err
	}
	if destination.Kind != Missing {
		if !overwrite && ignore {
			return false, nil
		}
		if !overwrite {
			return false, fmt.Errorf("rename destination exists: %s", to)
		}
		if source.Kind != destination.Kind {
			return false, errors.New("rename source/destination types differ")
		}
		if a, e := os.Stat(from); e == nil {
			if b, e := os.Stat(to); e == nil && os.SameFile(a, b) {
				return false, errors.New("case-only rename needs an intermediate distinct filename on this filesystem")
			}
		}
	}
	paths, err := p.tree(from)
	if err != nil {
		return false, err
	}
	moved := make(map[string]Node, len(paths))
	for _, at := range paths {
		rel, _ := filepath.Rel(from, at)
		target := filepath.Join(to, rel)
		if _, err = p.Read(target); err != nil {
			return false, err
		}
		moved[target] = p.After[at]
	}
	if destination.Kind != Missing {
		if _, err = p.Delete(to, true, false); err != nil {
			return false, err
		}
	}
	if err = p.parents(to); err != nil {
		return false, err
	}
	for _, at := range paths {
		p.After[at] = Node{}
	}
	for at, n := range moved {
		n.Listed = false
		n.Children = nil
		p.After[at] = n
	}
	return true, nil
}

func same(a, b Node) bool {
	return a.Kind == b.Kind && (a.Kind == Missing || (a.Data == b.Data && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID))
}

// Images returns independent maps suitable for guarded undo/redo. Directory
// membership for moved/deleted trees is derived from the planned final paths.
func (p *Plan) Images() (map[string]Node, map[string]Node) {
	before, after := map[string]Node{}, map[string]Node{}
	for path, n := range p.Before {
		before[path] = n
	}
	for path, n := range p.After {
		if n.Kind == Directory && (p.Before[path].Listed || p.Before[path].Kind != Directory) {
			n.Listed = true
			n.Children = nil
			for child, v := range p.After {
				if filepath.Dir(child) == path && v.Kind != Missing {
					n.Children = append(n.Children, filepath.Base(child))
				}
			}
			slices.Sort(n.Children)
		}
		after[path] = n
	}
	return before, after
}
