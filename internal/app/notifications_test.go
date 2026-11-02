package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNotifyLinuxDefaultAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := filepath.Join(dir, "notify-send")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MELO_NOTIFY_ARGS\"\nprintf '42\\n' >&3\nprintf 'default\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MELO_NOTIFY_ARGS", argsPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &app{}
	if !a.notifyLinux("My Song", "My Artist", nil) {
		t.Fatal("notification was not reported as shown")
	}
	waitNotifyID(t, a, 0)

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{"--id-fd=3", "--action=default=打开 Melo", "My Song", "My Artist"} {
		if !strings.Contains(args, want) {
			t.Fatalf("notify-send args missing %q:\n%s", want, args)
		}
	}
	if !strings.Contains(args, "--\nMy Song\nMy Artist\n") {
		t.Fatalf("notify-send args should end options with -- before title/body:\n%s", args)
	}
}

func TestNotifyLinuxClosedWithoutAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "notify-send")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '42\\n' >&3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &app{}
	if !a.notifyLinux("My Song", "My Artist", nil) {
		t.Fatal("notification was not reported as shown")
	}
	waitNotifyID(t, a, 0)
}

func TestNotifyLinuxFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "notify-send")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &app{}
	if a.notifyLinux("My Song", "My Artist", nil) {
		t.Fatal("failed notification was reported as shown")
	}
}

func waitNotifyID(t *testing.T, a *app, want uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		a.notifyMu.Lock()
		got := a.notifyID
		a.notifyMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification id = %d, want %d", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
