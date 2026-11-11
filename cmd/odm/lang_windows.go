//go:build windows

package main

import (
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// Tray menu strings. The language is the program's (progLang), set at tray
// start and changed live when the user picks another one in the window.
var trayStrings = map[string]map[string]string{
	"en": {
		"open": "Open ODM", "openTip": "Show the download window", "folder": "Open downloads folder",
		"autostart": "Start with Windows", "quit": "Quit ODM", "quitTip": "Stop the daemon and exit",
	},
	"tr": {
		"open": "ODM'yi Aç", "openTip": "İndirme penceresini göster", "folder": "İndirmeler klasörünü aç",
		"autostart": "Windows ile başlat", "quit": "ODM'den Çık", "quitTip": "Arka plan hizmetini durdur ve çık",
	},
}

var trayLang atomic.Value // string

const langTurkish = 0x1f

// systemLang picks the default language from Windows. Turkish if either the
// display language or the regional format is Turkish: many Turkish users run
// an English Windows with Turkish regional settings.
func systemLang() string {
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	ui, _, _ := k32.NewProc("GetUserDefaultUILanguage").Call()
	locale, _, _ := k32.NewProc("GetUserDefaultLangID").Call()
	if ui&0x3ff == langTurkish || locale&0x3ff == langTurkish {
		return "tr"
	}
	return "en"
}

func trayText(key string) string {
	l, _ := trayLang.Load().(string)
	if s, ok := trayStrings[l][key]; ok {
		return s
	}
	return trayStrings["en"][key]
}
