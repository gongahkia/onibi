package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func main() {
	input := flag.String("input", "internal/web/static", "static root directory")
	output := flag.String("output", "internal/web/static/dist-remote.zip", "remote static archive")
	flag.Parse()
	if err := run(*input, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input, output string) error {
	var paths []string
	for _, name := range []string{"dist", "fonts"} {
		files, err := collect(filepath.Join(input, name))
		if err != nil {
			return err
		}
		paths = append(paths, files...)
	}
	sort.Strings(paths)
	tmp, err := os.CreateTemp(filepath.Dir(output), ".dist-remote-*.zip")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	archive := zip.NewWriter(tmp)
	for _, path := range paths {
		if err := add(archive, input, path); err != nil {
			_ = archive.Close()
			_ = tmp.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, output); err != nil {
		return err
	}
	info, err := os.Stat(output)
	if err != nil {
		return err
	}
	fmt.Printf("remote static archive: %d bytes\n", info.Size())
	return nil
}

func collect(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", path)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func add(archive *zip.Writer, root, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	name, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	name = filepath.ToSlash(name)
	if info.IsDir() {
		name += "/"
	}
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
	if info.IsDir() {
		header.Method = zip.Store
		header.SetMode(0o755 | fs.ModeDir)
	} else {
		header.SetMode(0o644)
	}
	writer, err := archive.CreateHeader(header)
	if err != nil || info.IsDir() {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}
