//go:build windows

package main

import "golang.org/x/sys/windows"

// Tray menu strings, picked by the Windows display language. The Web UI and the
// extension have their own (switchable) translations.
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

// uiLang maps the user's Windows UI language to one we have strings for.
func uiLang() string {
	id, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	if id&0x3ff == 0x1f { // LANG_TURKISH
		return "tr"
	}
	return "en"
}

func trayText(key string) string {
	if s, ok := trayStrings[uiLang()][key]; ok {
		return s
	}
	return trayStrings["en"][key]
}
