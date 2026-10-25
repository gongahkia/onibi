package termui

import "time"

// Rattle ports the elapsed-time/offset/reverse mechanism and selected presets
// from vyfor/rattles (MIT), commit 6eabf671ee86ddd773f458b9c25ef9d3f00ae53b.
// See third_party/rattles/LICENSE-MIT. Rendering and lifecycle are Onibi's.
type Rattle struct {
	Frames   []string
	Interval time.Duration
	Offset   int
	Reverse  bool
}

func (r Rattle) Frame(elapsed time.Duration) string {
	if len(r.Frames) == 0 {
		return ""
	}
	interval := max(r.Interval, time.Millisecond)
	index := (int(max(elapsed, 0)/interval)%len(r.Frames) + r.Offset%len(r.Frames) + len(r.Frames)) % len(r.Frames)
	if r.Reverse {
		index = len(r.Frames) - 1 - index
	}
	return r.Frames[index]
}

var WaveRows = Rattle{Interval: 90 * time.Millisecond, Frames: []string{"⠖⠉⠉⠑", "⡠⠖⠉⠉", "⣠⡠⠖⠉", "⣄⣠⡠⠖", "⠢⣄⣠⡠", "⠙⠢⣄⣠", "⠉⠙⠢⣄", "⠊⠉⠙⠢", "⠜⠊⠉⠙", "⡤⠜⠊⠉", "⣀⡤⠜⠊", "⢤⣀⡤⠜", "⠣⢤⣀⡤", "⠑⠣⢤⣀", "⠉⠑⠣⢤", "⠋⠉⠑⠣"}}
var SimpleDots = Rattle{Interval: 400 * time.Millisecond, Frames: []string{".  ", ".. ", "...", "   "}}
