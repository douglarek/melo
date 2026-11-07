package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/locales"
)

func TestLanguageSetting(t *testing.T) {
	var applied []string
	orig := setAppLocale
	setAppLocale = func(tag string) { applied = append(applied, tag) }
	t.Cleanup(func() { setAppLocale = orig })

	a := newTestApp(t)
	a.update = updateStatus{version: "0.2.0"}
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if err := tt.Click("English"); err != nil {
		t.Fatalf("choose English: %v", err)
	}
	if a.settings.Language != "en" {
		t.Fatalf("Language = %q, want en", a.settings.Language)
	}
	if len(applied) != 1 || applied[0] != "en" {
		t.Fatalf("the app was told %v, want [en]", applied)
	}
	// The choice is what the app boots with, so it has to outlive the app.
	if got := loadSettings().Language; got != "en" {
		t.Fatalf("saved Language = %q, want en", got)
	}

	// Back to the user's languages. ("跟随系统" also names an appearance,
	// so the test says this one to the app rather than clicking it.)
	if !tt.HasText(locales.LanguageSystem()) {
		t.Fatalf("the language row should offer the system's language, texts: %v", tt.Texts())
	}
	a.setLanguage("")
	if a.settings.Language != "" {
		t.Fatalf("Language = %q, want the system's", a.settings.Language)
	}
	if len(applied) != 2 || applied[1] != "" {
		t.Fatalf("the app was told %v, want [en \"\"]", applied)
	}
}

// The catalogs say the same window in English: MYGO_LOCALE is what a test
// that never runs the app can choose a language with.
func TestEnglishTexts(t *testing.T) {
	t.Setenv("MYGO_LOCALE", "en")

	a := newTestApp(t)
	a.update = updateStatus{supported: true, version: "0.2.0"}
	a.settings.UpdateAvailable = "0.3.0"
	a.settingsModalOpen = true
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	for _, want := range []string{"Settings", "Appearance", "Language", "Software Update", "Check for Updates"} {
		if !tt.HasText(want) {
			t.Fatalf("the settings should say %q in English, texts: %v", want, tt.Texts())
		}
	}
	if tt.HasText("软件更新") || tt.HasText("自动检查更新") {
		t.Fatalf("English settings still show Chinese texts: %v", tt.Texts())
	}
	// The sidebar points at the version in English too.
	if !tt.HasText("Update 0.3.0") {
		t.Fatalf("the sidebar badge should be English, texts: %v", tt.Texts())
	}
	// Each language is named in itself, so an English reader can go back.
	if !tt.HasText("简体中文") {
		t.Fatalf("the language row should name Chinese in itself, texts: %v", tt.Texts())
	}
}
