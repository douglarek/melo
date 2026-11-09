package ytmusic

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func useTempSessionFile(t *testing.T) *[]byte {
	t.Helper()
	oldFile := sessionFile
	oldGet, oldSet, oldDelete := sessionSecretGet, sessionSecretSet, sessionSecretDelete
	var secret []byte
	path := filepath.Join(t.TempDir(), "session.json")
	sessionFile = func() (string, error) {
		return path, nil
	}
	sessionSecretGet = func() ([]byte, error) {
		if secret == nil {
			return nil, mygo.ErrSecretNotFound
		}
		return append([]byte(nil), secret...), nil
	}
	sessionSecretSet = func(v []byte) error {
		secret = append([]byte(nil), v...)
		return nil
	}
	sessionSecretDelete = func() error {
		secret = nil
		return nil
	}
	t.Cleanup(func() {
		_ = ClearSession()
		sessionFile = oldFile
		sessionSecretGet, sessionSecretSet, sessionSecretDelete = oldGet, oldSet, oldDelete
	})
	return &secret
}

func TestSaveSessionKeepsCookieInSecretStore(t *testing.T) {
	secret := useTempSessionFile(t)
	if err := SaveSession(&SessionData{Cookie: "SID=secret", Profile: &Profile{Name: "me"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if string(*secret) != "SID=secret" {
		t.Fatalf("secret = %q", *secret)
	}
	path, err := sessionFile()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SID=secret") {
		t.Fatalf("cookie leaked into the session file: %s", data)
	}
	loaded, err := LoadSession()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Cookie != "SID=secret" {
		t.Fatalf("loaded cookie = %q", loaded.Cookie)
	}
}

func TestLoadSessionMovesFileCookieToSecretStore(t *testing.T) {
	secret := useTempSessionFile(t)
	*secret = nil
	if err := writeSessionBytes([]byte(`{"cookie":"SID=legacy","profile":{"name":"me"}}`)); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Cookie != "SID=legacy" {
		t.Fatalf("loaded cookie = %q", loaded.Cookie)
	}
	if string(*secret) != "SID=legacy" {
		t.Fatalf("migrated secret = %q", *secret)
	}
	path, _ := sessionFile()
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "SID=legacy") {
		t.Fatalf("cookie stayed in the session file: %s", data)
	}
}

func TestSaveSessionFallsBackToFileWithoutSecretStore(t *testing.T) {
	useTempSessionFile(t)
	old := sessionSecretSet
	sessionSecretSet = func([]byte) error { return errors.ErrUnsupported }
	t.Cleanup(func() { sessionSecretSet = old })

	if err := SaveSession(&SessionData{Cookie: "SID=fallback"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path, _ := sessionFile()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "SID=fallback") {
		t.Fatalf("cookie missing from the fallback file: %s", data)
	}
	loaded, err := LoadSession()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Cookie != "SID=fallback" {
		t.Fatalf("loaded cookie = %q", loaded.Cookie)
	}
}

func cookieResponse(req *http.Request) *http.Response {
	header := make(http.Header)
	header.Add("Set-Cookie", "SID=refreshed; Path=/")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    req,
	}
}

func TestSetPlayerAuthOrigin(t *testing.T) {
	c := &Client{cookie: "SAPISID=test-sapisid; __Secure-3PAPISID=test-sapisid"}
	req := httptest.NewRequest(http.MethodPost, "https://music.youtube.com/youtubei/v1/player", nil)
	c.setPlayerAuthOrigin(req, "https://music.youtube.com")

	if got := req.Header.Get("Cookie"); !strings.Contains(got, "SAPISID=test-sapisid") {
		t.Fatalf("cookie = %q", got)
	}
	fields := strings.Fields(req.Header.Get("Authorization"))
	if len(fields) != 2 || fields[0] != "SAPISIDHASH" {
		t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
	}
	ts, digest, ok := strings.Cut(fields[1], "_")
	if !ok {
		t.Fatalf("authorization value = %q", fields[1])
	}
	sum := sha1.Sum([]byte(ts + " test-sapisid https://music.youtube.com"))
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %q, want the music.youtube.com hash", digest)
	}
}

func TestStaleCookieRefreshDoesNotOverwriteSession(t *testing.T) {
	useTempSessionFile(t)

	c := NewClient()
	started := make(chan struct{})
	release := make(chan struct{})
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return cookieResponse(req), nil
	})}

	if err := SaveSession(&SessionData{Cookie: "SID=old"}); err != nil {
		t.Fatalf("save old session: %v", err)
	}
	c.SetAuth("SID=old")

	done := make(chan error, 1)
	go func() {
		_, err := c.post(t.Context(), "test", nil, nil)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}

	if err := SaveSession(&SessionData{Cookie: "SID=new"}); err != nil {
		t.Fatalf("save new session: %v", err)
	}
	c.SetAuth("SID=new")
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("post: %v", err)
	}

	snapshot, _, ok := SessionSnapshot()
	if !ok || snapshot.Cookie != "SID=new" {
		t.Fatalf("session cookie = %q, want new session", snapshot.Cookie)
	}
	c.mu.RLock()
	clientCookie := c.cookie
	c.mu.RUnlock()
	if clientCookie != "SID=new" {
		t.Fatalf("client cookie = %q, want new session", clientCookie)
	}
}

func TestCookieRefreshAppliesToCurrentSession(t *testing.T) {
	useTempSessionFile(t)

	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return cookieResponse(req), nil
	})}

	if err := SaveSession(&SessionData{Cookie: "SID=old"}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	c.SetAuth("SID=old")

	if _, err := c.post(t.Context(), "test", nil, nil); err != nil {
		t.Fatalf("post: %v", err)
	}

	snapshot, _, ok := SessionSnapshot()
	if !ok || snapshot.Cookie != "SID=refreshed" {
		t.Fatalf("session cookie = %q, want refreshed cookie", snapshot.Cookie)
	}
	c.mu.RLock()
	clientCookie := c.cookie
	c.mu.RUnlock()
	if clientCookie != "SID=refreshed" {
		t.Fatalf("client cookie = %q, want refreshed cookie", clientCookie)
	}
}
