package app

import (
	"bufio"
	"github.com/douglarek/melo/locales"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/douglarek/melo/internal/ytmusic"
)

func (a *app) maybeNotifyTrack(t *ytmusic.Track) {
	if t == nil {
		a.notifyTrackID = ""
		return
	}
	if t.ID == a.notifyTrackID {
		return
	}
	a.notifyTrackID = t.ID
	if !a.settings.TrackNotifications || a.win == nil || a.win.IsFocused() {
		return
	}
	if !a.pl.Playing() && !a.pl.PendingPlay() {
		return
	}
	a.notifySeq++
	seq := a.notifySeq
	go a.notifyTrack(t, seq)
}

func (a *app) notifyTrack(t *ytmusic.Track, seq int) {
	icon := a.notificationIcon(t)
	if seq != a.notifySeq {
		return
	}
	body := t.ArtistNames()
	if runtime.GOOS == "linux" && a.notifyLinux(t.Title, body, icon) {
		return
	}
	opts := mygo.NotificationOptions{
		ID:     "melo-track",
		Title:  t.Title,
		Body:   body,
		Silent: true,
	}
	if len(icon) > 0 {
		opts.Sender = &mygo.NotificationSender{ID: "melo-" + t.ID, Name: t.Title, Avatar: icon}
	}
	n := mygo.NewNotification(opts)
	n.OnClick(func() {
		a.showFromNotification()
		n.Close()
	})
	_ = n.Show()
}

func (a *app) showFromNotification() {
	a.post(func() {
		if a.win == nil {
			return
		}
		a.win.Show()
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Focus()
		mygo.App.Focus()
	})
}

// notificationIcon waits briefly for the artwork cache to decode the cover.
func (a *app) notificationIcon(t *ytmusic.Track) []byte {
	if t.Thumbnail == "" {
		return nil
	}
	for i := 0; i < 20; i++ {
		art := a.art.get(t.Thumbnail, artNotification)
		if art != nil {
			if art.failed || len(art.data) == 0 {
				return nil
			}
			return append([]byte(nil), art.data...)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func (a *app) notifyLinux(title, body string, icon []byte) bool {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return false
	}

	iconPath := ""
	if len(icon) > 0 {
		f, err := os.CreateTemp("", "melo-notify-*.jpg")
		if err == nil {
			if _, err = f.Write(icon); err == nil {
				iconPath = f.Name()
			}
			_ = f.Close()
			if iconPath == "" {
				_ = os.Remove(f.Name())
			}
		}
	}

	idR, idW, err := os.Pipe()
	if err != nil {
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return false
	}

	a.notifyMu.Lock()
	replace := a.notifyID
	a.notifyMu.Unlock()

	args := []string{
		"--app-name=Melo",
		"--urgency=low",
		"--id-fd=3",
		locales.NotificationShowAction(),
	}
	if replace != 0 {
		args = append(args, "--replace-id="+strconv.FormatUint(uint64(replace), 10))
	}
	if iconPath != "" {
		args = append(args, "--icon="+iconPath)
	}
	args = append(args, "--", title, body)

	cmd := exec.Command(path, args...)
	cmd.ExtraFiles = []*os.File{idW}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = idR.Close()
		_ = idW.Close()
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return false
	}
	if err := cmd.Start(); err != nil {
		_ = idR.Close()
		_ = idW.Close()
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return false
	}
	_ = idW.Close()

	id, err := readNotificationID(idR)
	_ = idR.Close()
	if err != nil || id == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
		return false
	}

	a.notifyMu.Lock()
	a.notifyID = id
	a.notifyMu.Unlock()

	go func() {
		out, _ := io.ReadAll(stdout)
		_ = cmd.Wait()
		if strings.Contains(string(out), "default") {
			a.showFromNotification()
		}
		a.notifyMu.Lock()
		if a.notifyID == id {
			a.notifyID = 0
		}
		a.notifyMu.Unlock()
		if iconPath != "" {
			_ = os.Remove(iconPath)
		}
	}()
	if iconPath != "" {
		time.AfterFunc(5*time.Minute, func() { _ = os.Remove(iconPath) })
	}
	return true
}

func readNotificationID(r io.Reader) (uint32, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(id), nil
}
