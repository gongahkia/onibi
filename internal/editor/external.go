package editor

import (
	"github.com/gdamore/tcell/v2"
)

func (a *App) checkDisk() {
	b := a.Current()
	if a.fileCheckBusy || b.File.Path == "" || b.Transient {
		return
	}
	a.fileCheckBusy = true
	file := *b.File
	limit := int64(a.Config.MaxFileMiB) << 20
	go func() {
		err := file.Unchanged(limit)
		a.post(func() {
			a.fileCheckBusy = false
			if *b.File != file {
				return
			}
			b.DiskConflict = err != nil
			if err != nil && b == a.Current() {
				a.Status = "File changed on disk · Reload from disk or Save as"
			}
		})
	}()
}
func (a *App) focusEvent(ev *tcell.EventFocus) {
	if ev.Focused {
		a.checkDisk()
	}
}
