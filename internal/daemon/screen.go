package daemon

import (
	"errors"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/render"
)

func (d *Daemon) screenPNGOptions(rows, cols int) render.PNGOptions {
	d.screenMu.RLock()
	fontName, fontPath := d.ScreenFont, d.ScreenFontPath
	d.screenMu.RUnlock()
	return render.PNGOptions{Rows: rows, Cols: cols, Scale: render.DefaultScale, Font: fontName, FontPath: fontPath}
}

func (d *Daemon) screenFont() (string, string) {
	d.screenMu.RLock()
	defer d.screenMu.RUnlock()
	return d.ScreenFont, d.ScreenFontPath
}

func (d *Daemon) SetScreenFont(name string) error {
	name = strings.TrimSpace(name)
	if d.Paths.Config == "" {
		return errors.New("screen configuration unavailable")
	}
	cfg, _, err := config.Load(d.Paths)
	if err != nil {
		return err
	}
	cfg.Screen.Font = name
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := render.ValidateFont(cfg.Screen.Font, cfg.Screen.FontPath); err != nil {
		return err
	}
	if err := config.Save(d.Paths.Config, cfg); err != nil {
		return err
	}
	d.screenMu.Lock()
	d.ScreenFont, d.ScreenFontPath = cfg.Screen.Font, cfg.Screen.FontPath
	d.screenMu.Unlock()
	return nil
}

func (d *Daemon) screenFontChoices() []render.FontChoice {
	choices := append([]render.FontChoice(nil), render.BuiltinFonts()...)
	_, path := d.screenFont()
	if strings.TrimSpace(path) != "" {
		choices = append(choices, render.FontChoice{ID: render.FontCustom, Label: "External font"})
	}
	return choices
}
