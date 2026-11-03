package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestExecutable(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "ffmpeg")
	writeFile(t, run, 0o755)
	if !executable(run) {
		t.Fatal("a file that can run was not found executable")
	}

	plain := filepath.Join(dir, "plain")
	writeFile(t, plain, 0o644)
	if executable(plain) {
		t.Fatal("a file that cannot run was found executable")
	}
	if executable(dir) {
		t.Fatal("a directory was found executable")
	}
	if executable("") {
		t.Fatal("an empty path was found executable")
	}
}

func TestLookupPrefersTheOverride(t *testing.T) {
	run := filepath.Join(t.TempDir(), "ffmpeg")
	writeFile(t, run, 0o755)
	t.Setenv("MELO_FFMPEG", run)

	got, err := lookup()
	if err != nil || got != run {
		t.Fatalf("lookup = %q, %v, want %q", got, err, run)
	}
}

func TestLookupSkipsAnOverrideThatCannotRun(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "ffmpeg")
	writeFile(t, plain, 0o644)
	t.Setenv("MELO_FFMPEG", plain)

	if got, err := lookup(); err == nil && got == plain {
		t.Fatal("lookup took an override that cannot run")
	}
}

func TestInstallDirsCoverHomebrew(t *testing.T) {
	dirs := installDirs()
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		found := false
		for _, dir := range dirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("installDirs = %v, want %s among them", dirs, want)
		}
	}
}
