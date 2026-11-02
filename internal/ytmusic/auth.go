package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
)

// SessionData holds the persisted credentials and user info.
type SessionData struct {
	Cookie  string   `json:"cookie,omitempty"`
	Profile *Profile `json:"profile,omitempty"`
}

var (
	sessionMu      sync.RWMutex
	currentSession *SessionData
)

func sessionFile() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func readSessionBytes() ([]byte, error) {
	path, err := sessionFile()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeSessionBytes(b []byte) error {
	path, err := sessionFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func deleteSessionBytes() {
	path, err := sessionFile()
	if err == nil {
		_ = os.Remove(path)
	}
}

// SaveSession persists the session data.
func SaveSession(sess *SessionData) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	currentSession = sess

	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return writeSessionBytes(b)
}

// LoadSession restores saved session data.
func LoadSession() (*SessionData, error) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	b, err := readSessionBytes()
	if err != nil || len(b) == 0 {
		return nil, errors.New("no saved session")
	}

	var sess SessionData
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, err
	}

	currentSession = &sess
	DefaultClient.SetAuth(sess.Cookie)
	return &sess, nil
}

// ClearSession clears the stored session.
func ClearSession() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	currentSession = nil
	DefaultClient.SetAuth("")
	deleteSessionBytes()
}

// CurrentSession returns the active in-memory session.
func CurrentSession() *SessionData {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return currentSession
}

// Account fetches the logged-in user profile from YouTube Music.
func (c *Client) Account(ctx context.Context) (*Profile, error) {
	data, err := c.post(ctx, "account/account_menu", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	actions, _ := root["actions"].([]any)
	for _, a := range actions {
		aMap, ok := a.(map[string]any)
		if !ok {
			continue
		}
		popupAction, ok := aMap["openPopupAction"].(map[string]any)
		if !ok {
			continue
		}
		popup, ok := popupAction["popup"].(map[string]any)
		if !ok {
			continue
		}
		menu, ok := popup["multiPageMenuRenderer"].(map[string]any)
		if !ok {
			continue
		}
		headerObj, ok := menu["header"].(map[string]any)
		if !ok {
			continue
		}
		header, _ := headerObj["activeAccountHeaderRenderer"].(map[string]any)
		if header != nil {
			name := RunsToString(header["accountName"])
			handle := RunsToString(header["channelHandle"])
			thumbs := ParseThumbnails(header["accountPhoto"])
			return &Profile{
				Name:   name,
				Handle: handle,
				Avatar: BestThumbnail(thumbs),
			}, nil
		}
	}

	return nil, errors.New("profile not found in account menu")
}
