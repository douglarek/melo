package app

import (
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
