package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

type settings struct {
	Appearance         string   `json:"appearance"` // "system", "dark", "light"
	SearchHistory      []string `json:"searchHistory,omitempty"`
	CacheMB            int64    `json:"cacheMB"`
	CloseToTray        bool     `json:"closeToTray"`
	TrackNotifications bool     `json:"trackNotifications"`
	// Language is the language Melo shows: "" follows the system's, or a
	// tag of those it is written in ("zh-Hans", "en").
	Language string `json:"language,omitempty"`
	// CheckUpdates reports whether Melo looks for new versions in the
	// background, and points at them in the sidebar and the tray. It is
	// off by default: a version that cannot replace itself is updated
	// elsewhere, and has nothing to check for.
	CheckUpdates bool `json:"checkUpdates"`
	// UpdateLastCheck is when a check last found out whether a newer
	// version is out.
	UpdateLastCheck time.Time `json:"updateLastCheck,omitzero"`
	// UpdateAvailable is the newer version that the last check found, and
	// "" once there is none, or once the user skipped it.
	UpdateAvailable string `json:"updateAvailable,omitempty"`
	// UpdateSkipped is the version the user skipped in the update window,
	// which background checks do not point at again.
	UpdateSkipped string `json:"updateSkipped,omitempty"`
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
	dir, err := userDataDir()
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
