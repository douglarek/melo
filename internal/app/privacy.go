package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// secureUserDataDir tightens the permissions of data written by MyGo and
// WebKitGTK. Older builds could leave the user data directory as 0755 and
// cookies.sqlite as 0644, which can expose the Google session to other
// local users.
func secureUserDataDir() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || dir == "" {
		return
	}
	secureUserDataDirAt(dir)
}

func secureUserDataDirAt(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.Chmod(dir, 0o700)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "cookies.sqlite") ||
			name == "session.json" ||
			name == "settings.json" ||
			name == "library.json" ||
			name == "player.json" {
			_ = os.Chmod(filepath.Join(dir, name), 0o600)
		}
	}
}

// clearDefaultBrowserData removes what the login window stored in the
// default WebView session. New logins use an ephemeral session; this also
// cleans up cookies left by older builds.
func (a *app) clearDefaultBrowserData() {
	if a.win == nil {
		return
	}
	go func() {
		_ = mygo.DefaultSession().ClearData(mygo.SiteDataAll)
	}()
}

// webViewClearedMark names the file that says the cookies older builds left in
// the persistent WebView session are gone.
const webViewClearedMark = "webview-cleared"

// webViewCleared reports whether those cookies were dropped already.
func webViewCleared() bool {
	path, err := userDataFile(webViewClearedMark)
	if err != nil {
		return true // nowhere to keep the mark: do not clear on every launch
	}
	_, err = os.Stat(path)
	return err == nil
}

// markWebViewCleared records that they were.
func markWebViewCleared() {
	path, err := userDataFile(webViewClearedMark)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, []byte("1\n"), 0o600)
}
