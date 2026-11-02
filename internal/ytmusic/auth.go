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

// ErrSessionStale means a response belongs to a session that has since been
// replaced or cleared.
var ErrSessionStale = errors.New("ytmusic: stale session")

var (
	sessionMu      sync.RWMutex
	currentSession *SessionData
	sessionGen     uint64
)

var sessionFile = func() (string, error) {
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func deleteSessionBytes() error {
	path, err := sessionFile()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func cloneSession(sess *SessionData) *SessionData {
	if sess == nil {
		return nil
	}
	clone := *sess
	if sess.Profile != nil {
		profile := *sess.Profile
		clone.Profile = &profile
	}
	return &clone
}

// SaveSession persists session data and makes it current.
func SaveSession(sess *SessionData) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if sess == nil {
		return errors.New("ytmusic: nil session")
	}

	clone := cloneSession(sess)
	b, err := json.Marshal(clone)
	if err != nil {
		return err
	}
	if err := writeSessionBytes(b); err != nil {
		return err
	}
	currentSession = clone
	sessionGen++
	return nil
}

// LoadSession restores saved session data.
func LoadSession() (*SessionData, error) {
	b, err := readSessionBytes()
	if err != nil || len(b) == 0 {
		return nil, errors.New("no saved session")
	}

	var sess SessionData
	if err := json.Unmarshal(b, &sess); err != nil {
		return nil, err
	}

	sessionMu.Lock()
	currentSession = cloneSession(&sess)
	sessionGen++
	sessionMu.Unlock()

	DefaultClient.SetAuth(sess.Cookie)
	return cloneSession(&sess), nil
}

// ClearSession clears the in-memory session and removes the persisted file.
func ClearSession() error {
	sessionMu.Lock()
	currentSession = nil
	sessionGen++
	sessionMu.Unlock()

	DefaultClient.SetAuth("")
	return deleteSessionBytes()
}

// CurrentSession returns a snapshot of the active in-memory session.
func CurrentSession() *SessionData {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return cloneSession(currentSession)
}

// SessionSnapshot returns a snapshot of the current session and the generation
// that identifies it.
func SessionSnapshot() (SessionData, uint64, bool) {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	if currentSession == nil {
		return SessionData{}, 0, false
	}
	return *cloneSession(currentSession), sessionGen, true
}

func sessionGeneration() (uint64, bool) {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionGen, currentSession != nil
}

// UpdateSessionCookie applies a cookie refresh to the matching session
// generation.
func UpdateSessionCookie(gen uint64, cookie string) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if currentSession == nil || sessionGen != gen {
		return ErrSessionStale
	}

	clone := cloneSession(currentSession)
	clone.Cookie = cookie
	b, err := json.Marshal(clone)
	if err != nil {
		return err
	}
	if err := writeSessionBytes(b); err != nil {
		return err
	}
	currentSession = clone
	return nil
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
