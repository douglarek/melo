package app

import (
	"net/url"
	"strings"
	"testing"
)

// TestPotPageCarriesItsClient checks that the token page loads with the
// BotGuard client in it: a window opens for a mint and closes after, and the
// page it loads asks for nothing.
func TestPotPageCarriesItsClient(t *testing.T) {
	page := potPage()
	if !strings.Contains(page, "window.mintPoToken") {
		t.Fatal("the page does not define the minter")
	}
	if strings.Contains(page, `src="bgutils.js"`) {
		t.Fatal("the page requests its client rather than carrying it")
	}
	if !strings.Contains(page, "window.potReady = true") {
		t.Fatal("the page does not say when it is ready")
	}
}

func TestCheckPotURL(t *testing.T) {
	allowed := []string{
		"https://www.youtube.com/api/jnn/v1/Create",
		"https://jnn-pa.googleapis.com/$rpc/google.internal.waa.v1.Waa/Create",
		"https://WWW.YOUTUBE.COM:443/api/jnn/v1/GenerateIT",
	}
	for _, raw := range allowed {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := checkPotURL(u); err != nil {
			t.Errorf("checkPotURL(%q) = %v, want nil", raw, err)
		}
	}

	denied := []string{
		"http://www.youtube.com/",
		"https://127.0.0.1/",
		"https://localhost/",
		"https://evil.example/",
		"https://www.youtube.com.evil.example/",
		"https://www.youtube.com:8443/",
		"https://user@www.youtube.com/",
		"https://jnn-pa.googleapis.com@evil.example/",
	}
	for _, raw := range denied {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := checkPotURL(u); err == nil {
			t.Errorf("checkPotURL(%q) = nil, want an error", raw)
		}
	}
}
