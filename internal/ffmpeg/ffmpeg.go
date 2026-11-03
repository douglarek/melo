// Package ffmpeg locates the ffmpeg binary, in the places package managers
// install into as well as on PATH.
package ffmpeg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// ErrNotFound says no ffmpeg was found where it can be.
var ErrNotFound = errors.New("ffmpeg not found")

// found is the path looked up, which is kept so that Path is cheap.
var found atomic.Value // string

// Path returns ffmpeg's path when it is known, looking for it on the
// process's PATH and in the places package managers install into. It never
// runs a shell, so it may be called on the UI thread; Resolve finds an
// install that is nowhere else.
func Path() (string, bool) {
	if p, ok := found.Load().(string); ok && p != "" {
		return p, true
	}
	p, err := lookup()
	if err != nil {
		return "", false
	}
	found.Store(p)
	return p, true
}

// Resolve finds ffmpeg, asking the login shell for its PATH as a last
// resort, which can take a moment: call it off the UI thread.
func Resolve(ctx context.Context) (string, error) {
	if p, ok := Path(); ok {
		return p, nil
	}
	if p, ok := shellPath(ctx); ok {
		found.Store(p)
		return p, nil
	}
	return "", ErrNotFound
}

// lookup checks the overrides first, then the process's PATH, then the
// places package managers install into.
func lookup() (string, error) {
	for _, key := range []string{"MELO_FFMPEG", "FFMPEG_PATH"} {
		if p := fromEnv(key); p != "" {
			return p, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	for _, dir := range installDirs() {
		if p := filepath.Join(dir, "ffmpeg"); executable(p) {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// fromEnv returns the executable path a variable names, or "" for none
// that runs.
func fromEnv(key string) string {
	p := strings.TrimSpace(os.Getenv(key))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		abs, err := exec.LookPath(p)
		if err != nil {
			return ""
		}
		p = abs
	}
	if !executable(p) {
		return ""
	}
	return p
}

// installDirs returns the directories Homebrew, MacPorts and Linuxbrew
// install into, which a window started from Finder does not have on PATH.
func installDirs() []string {
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	if runtime.GOOS == "linux" {
		dirs = append(dirs, "/usr/bin", "/snap/bin", "/home/linuxbrew/.linuxbrew/bin")
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	return dirs
}

// shellPath asks the login shell where ffmpeg is, for an install the
// known directories do not hold.
func shellPath(ctx context.Context) (string, bool) {
	if runtime.GOOS == "windows" {
		return "", false
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", "command -v ffmpeg").Output()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if !executable(line) {
		return "", false
	}
	return line, true
}

// executable reports whether p is a file that can run.
func executable(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode().Perm()&0o111 != 0
}
