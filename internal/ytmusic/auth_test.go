package ytmusic

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func useTempSessionFile(t *testing.T) {
	t.Helper()
	old := sessionFile
	sessionFile = func() (string, error) {
		return filepath.Join(t.TempDir(), "session.json"), nil
	}
	t.Cleanup(func() {
		sessionFile = old
		_ = ClearSession()
	})
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
