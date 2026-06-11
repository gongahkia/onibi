package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/spf13/cobra"
)

func runTailLog(cmd *cobra.Command, _ []string) error {
	n, _ := cmd.Flags().GetInt("n")
	follow, _ := cmd.Flags().GetBool("follow")
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	path := filepath.Join(paths.LogDir, "onibi.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", path)
	} else if err != nil {
		return err
	}
	if err := printTail(cmd.OutOrStdout(), path, n); err != nil {
		return err
	}
	if follow {
		return followFile(cmd.OutOrStdout(), path)
	}
	return nil
}

func printTail(w io.Writer, path string, n int) error {
	if n <= 0 {
		n = 80
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	lines := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(lines) == n {
			copy(lines, lines[1:])
			lines[n-1] = sc.Text()
			continue
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	return nil
}

func followFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}
