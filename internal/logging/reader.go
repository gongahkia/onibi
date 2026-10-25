package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/termui"
	"golang.org/x/sys/unix"
)

const maxTailBytes = 1 << 20

// ReadLogs reads a bounded tail, then optionally follows the current inode and
// reopens after rotation. It never follows symlinks or reads terminal output.
func ReadLogs(ctx context.Context, path string, tail int, follow bool, emit func([]byte) error) error {
	if tail < 0 || tail > 1000 {
		return errors.New("--tail must be between 0 and 1000")
	}
	open := func() (*os.File, error) {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), path)
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return nil, errors.New("log path is not a regular file")
		}
		return f, nil
	}
	file, err := open()
	if err != nil {
		return err
	}
	defer func() { file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	start := max(int64(0), info.Size()-maxTailBytes)
	if _, err = file.Seek(start, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTailBytes))
	if err != nil {
		return err
	}
	if start > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
			data = data[idx+1:]
		} else {
			data = nil
		}
	}
	var pending []byte
	if idx := bytes.LastIndexByte(data, '\n'); idx >= 0 {
		pending = append(pending, data[idx+1:]...)
		data = data[:idx]
	} else {
		pending = data
		data = nil
	}
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines[max(0, len(lines)-tail):] {
		if len(line) > 0 {
			if err := emit(line); err != nil {
				return err
			}
		}
	}
	if !follow {
		return nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	buf := make([]byte, 32<<10)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		// Drain old file before switching; a partial record stays buffered.
		for {
			if err := ctx.Err(); err != nil {
				return nil
			}
			n, readErr := file.Read(buf)
			pending = append(pending, buf[:n]...)
			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				if err := emit(pending[:idx]); err != nil {
					return err
				}
				pending = pending[idx+1:]
			}
			if len(pending) > maxTailBytes {
				return errors.New("log record exceeds inspection limit")
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		current, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		position, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if !os.SameFile(info, current) || current.Size() < position {
			next, err := open()
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			file.Close()
			file = next
			info, err = file.Stat()
			if err != nil {
				return err
			}
			pending = nil
		}
	}
}

// FormatRecord provides a safe, deterministic console view of JSON telemetry.
func FormatRecord(line []byte) string {
	var record map[string]any
	if json.Unmarshal(line, &record) != nil {
		return "[unreadable log record]"
	}
	timestamp, _ := record["time"].(string)
	if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		timestamp = parsed.Local().Format("15:04:05")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %-5v  %v", timestamp, record["level"], record["msg"])
	keys := make([]string, 0, len(record))
	for key := range record {
		if key != "time" && key != "level" && key != "msg" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "  %s=%v", key, record[key])
	}
	return termui.Clean(b.String())
}
