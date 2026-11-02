package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

type settings struct {
	Appearance         string   `json:"appearance"` // "system", "dark", "light"
	SearchHistory      []string `json:"searchHistory,omitempty"`
	CacheMB            int64    `json:"cacheMB"`
	CloseToTray        bool     `json:"closeToTray"`
	TrackNotifications bool     `json:"trackNotifications"`
}

func defaultSettings() settings {
	return settings{
		Appearance:         "system",
		CacheMB:            1024,
		CloseToTray:        true,
		TrackNotifications: true,
	}
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func loadSettings() settings {
	s := defaultSettings()
	path, err := settingsPath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func (s *settings) save() {
	path, err := settingsPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return
	}
	_ = os.Chmod(path, 0o600)
}

var setThemeSource = func(src mygo.ThemeSource) {
	mygo.Theme.SetSource(src)
}

func (s *settings) applyAppearance() {
	switch s.Appearance {
	case "dark":
		setThemeSource(mygo.ThemeDark)
	case "light":
		setThemeSource(mygo.ThemeLight)
	default:
		setThemeSource(mygo.ThemeSystem)
	}
}
