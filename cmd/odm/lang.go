package main

import (
	"errors"

	"github.com/aoztrk82/open-download-manager/internal/config"
)

// progLang is the program's UI language: the choice saved in prefs.json, or
// the system language until the user picks one in the window (View menu).
// The window, the Web UI and the tray all follow it.
type progLang struct {
	onChange func(lang string) // tray: retitle the menu; may be nil
}

func (p progLang) Lang() string {
	if l := config.LoadPrefs().Lang; config.KnownLang(l) {
		return l
	}
	return systemLang()
}

func (p progLang) SetLang(lang string) error {
	if !config.KnownLang(lang) {
		return errors.New("unknown language")
	}
	pr := config.LoadPrefs()
	pr.Lang = lang
	if err := config.SavePrefs(pr); err != nil {
		return err
	}
	if p.onChange != nil {
		p.onChange(lang)
	}
	return nil
}
