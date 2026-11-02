package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecureUserDataDirAt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not map to Windows ACLs")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	names := []string{
		"cookies.sqlite",
		"cookies.sqlite-wal",
		"session.json",
		"settings.json",
		"library.json",
		"player.json",
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("cookie"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	secureUserDataDirAt(dir)

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Fatalf("data dir mode = %o, want 700", got)
	}
	for _, name := range names {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", name, got)
		}
	}
}
