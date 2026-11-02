package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/douglarek/melo/internal/ytmusic"
)

// openHEAACStream transcodes an HE-AAC source to AAC-LC, then plays the
// result through the pure Go M4A decoder. ffmpeg is used on every platform;
// macOS can fall back to the built-in afconvert.
func openHEAACStream(ctx context.Context, info *ytmusic.StreamInfo, cache *Cache) (*mp4Stream, error) {
	lcPath := cache.Path(info.VideoID, "heaac.m4a")
	if st, err := openMP4Stream(lcPath); err == nil {
		return st, nil
	}
	_ = os.Remove(lcPath)

	srcPath := cache.Path(info.VideoID, "heaac.mp4")
	if err := DownloadToFile(ctx, info.DirectURL, srcPath); err != nil {
		return nil, err
	}

	tmpPath := lcPath + ".tmp"
	_ = os.Remove(tmpPath)
	if err := transcodeHEAAC(ctx, srcPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, lcPath); err != nil {
		return nil, err
	}
	return openMP4Stream(lcPath)
}

func transcodeHEAAC(ctx context.Context, src, dst string) error {
	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-i", src, "-vn", "-c:a", "aac", "-b:a", "128k", "-f", "mp4", dst)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("HE-AAC transcode with ffmpeg: %w: %s", err, out)
		}
		return nil
	}
	if runtime.GOOS == "darwin" {
		if afconvert, err := exec.LookPath("afconvert"); err == nil {
			cmd := exec.CommandContext(ctx, afconvert, "-f", "m4af", "-d", "aac", "-b", "128000", src, dst)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("HE-AAC transcode with afconvert: %w: %s", err, out)
			}
			return nil
		}
	}
	return errors.New("HE-AAC playback requires ffmpeg")
}
