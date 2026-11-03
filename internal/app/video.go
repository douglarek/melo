package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/douglarek/melo/internal/ffmpeg"
	"github.com/douglarek/melo/internal/ytmusic"
)

const (
	videoFrameW = 1280
	videoFrameH = 720
	videoFPS    = 30
	videoUA     = "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip"
)

type ffmpegVideoDecoder struct {
	url       string
	userAgent string
	start     time.Duration
	path      string
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderr    bytes.Buffer
	frame     []byte
	img       *image.RGBA
	closed    bool
}

func newFFmpegVideoDecoder(url, userAgent string, start time.Duration) (*ffmpegVideoDecoder, error) {
	path, err := ffmpeg.Resolve(context.Background())
	if err != nil {
		return nil, err
	}
	d := &ffmpegVideoDecoder{
		url:       url,
		userAgent: userAgent,
		start:     start,
		path:      path,
		frame:     make([]byte, videoFrameW*videoFrameH*4),
		img:       image.NewRGBA(image.Rect(0, 0, videoFrameW, videoFrameH)),
	}
	if err := d.startProcess(path); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *ffmpegVideoDecoder) startProcess(path string) error {
	args := []string{
		"-v", "error",
		"-nostdin",
	}
	if strings.HasPrefix(d.url, "http") && d.userAgent != "" {
		args = append(args, "-user_agent", d.userAgent)
	}
	args = append(args, "-fflags", "nobuffer", "-probesize", "1M", "-analyzeduration", "0")
	if d.start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(d.start.Seconds(), 'f', 3, 64))
	}
	args = append(args,
		"-i", d.url,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", videoFrameW, videoFrameH, videoFrameW, videoFrameH),
		"-r", strconv.Itoa(videoFPS),
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"pipe:1",
	)
	cmd := exec.Command(path, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	d.stderr.Reset()
	cmd.Stderr = &d.stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	d.cmd = cmd
	d.stdout = stdout
	return nil
}

func (d *ffmpegVideoDecoder) Next() (ui.VideoFrame, error) {
	if d.stdout == nil {
		return ui.VideoFrame{}, io.EOF
	}
	if _, err := io.ReadFull(d.stdout, d.frame); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if msg := bytes.TrimSpace(d.stderr.Bytes()); len(msg) > 0 {
				return ui.VideoFrame{}, fmt.Errorf("ffmpeg video: %s", msg)
			}
			return ui.VideoFrame{}, io.EOF
		}
		return ui.VideoFrame{}, err
	}
	copy(d.img.Pix, d.frame)
	return ui.VideoFrame{Image: d.img, Duration: time.Second / videoFPS}, nil
}

func (d *ffmpegVideoDecoder) Rewind() error {
	d.Close()
	d.closed = false
	d.start = 0
	return d.startProcess(d.path)
}

func (d *ffmpegVideoDecoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.stdout != nil {
		_ = d.stdout.Close()
		d.stdout = nil
	}
	if d.cmd != nil {
		_ = d.cmd.Process.Kill()
		cmd := d.cmd
		d.cmd = nil
		go cmd.Wait()
	}
	return nil
}

func (a *app) resetVideoForTrack(cur *ytmusic.Track) {
	id := ""
	if cur != nil {
		id = cur.ID
	}
	if id == a.lastPlayedTrackID {
		return
	}
	a.lastPlayedTrackID = id
	a.npVideoMode = false
	a.npVideoKick = ""
	a.closeVideo()
}

func (a *app) closeVideo() {
	a.npVideoSeq++
	a.npVideoStarting = false
	a.npVideoTrack = ""
	if a.npVideo != nil {
		vp := a.npVideo
		a.npVideo = nil
		// VideoPlayer.Close waits for its decoder; keep it off the UI thread.
		go vp.Close()
	}
}

type primedVideoDecoder struct {
	dec      *ffmpegVideoDecoder
	first    ui.VideoFrame
	hasFirst bool
	sent     bool
}

func (p *primedVideoDecoder) Next() (ui.VideoFrame, error) {
	if !p.sent && p.hasFirst {
		p.sent = true
		return p.first, nil
	}
	return p.dec.Next()
}

func (p *primedVideoDecoder) Rewind() error {
	p.sent = false
	return p.dec.Rewind()
}

func (p *primedVideoDecoder) Close() error {
	return p.dec.Close()
}

func (a *app) videoSource(info *ytmusic.StreamInfo) string {
	if a.pl.Cache != nil && info.ContentLength > 0 {
		path := a.pl.Cache.Path(info.VideoID, "mp4")
		if st, err := os.Stat(path); err == nil && st.Size() >= info.ContentLength {
			return path
		}
	}
	return info.DirectURL
}

// ensureVideo decides whether the video pane can be shown and keeps it in
// sync with the audio transport. Starting ffmpeg happens off the UI thread;
// the video pane shows a spinner until the first frame is ready.
func (a *app) ensureVideo(c *ui.Context) {
	cur := a.pl.Current()
	if cur == nil || !cur.Video || !a.npVideoMode {
		a.closeVideo()
		return
	}
	if a.npVideo != nil && a.npVideoTrack == cur.ID {
		if a.pl.Playing() {
			a.npVideo.Play()
		} else {
			a.npVideo.Pause()
		}
		return
	}
	if a.npVideoStarting {
		return
	}

	info := a.pl.StreamInfo()
	if info == nil {
		if a.pl.Loading() || a.pl.PendingPlay() {
			return // the pane shows its spinner
		}
		// Nothing has resolved the stream, as when a restart restores the
		// queue: start it, as the video needs a stream as much as the
		// speakers do.
		if a.npVideoKick != cur.ID {
			a.npVideoKick = cur.ID
			a.pl.Play()
		}
		return
	}
	// The stream chosen for the speakers carries a picture when it is a
	// progressive one; when it is not, one is resolved for the pane alone.
	source, userAgent := "", videoUA
	if info.HasVideo && info.DirectURL != "" {
		source = a.videoSource(info)
	}

	a.closeVideo()
	a.npVideoStarting = true
	a.npVideoSeq++
	seq := a.npVideoSeq
	curID := cur.ID
	start := a.pl.Position()
	a.npVideoTrack = curID

	go func() {
		url, ua := source, userAgent
		if url == "" {
			vs, err := ytmusic.DefaultClient.ResolveVideo(context.Background(), curID)
			if err != nil {
				a.post(func() {
					if seq != a.npVideoSeq {
						return
					}
					a.npVideoStarting = false
					a.npVideoMode = false
					c.Toast("当前视频无法解码，继续以音频播放")
					c.Invalidate()
				})
				return
			}
			url, ua = vs.URL, vs.UserAgent
		}
		dec, err := newFFmpegVideoDecoder(url, ua, start)
		if err != nil {
			a.post(func() {
				if seq != a.npVideoSeq {
					return
				}
				a.npVideoStarting = false
				a.npVideoMode = false
				if errors.Is(err, ffmpeg.ErrNotFound) {
					c.Toast("未找到 ffmpeg，无法观看音乐视频。macOS 可用 brew install ffmpeg 安装，或用 MELO_FFMPEG 指定可执行文件路径。")
				} else {
					c.Toast("视频加载失败，继续以音频播放")
				}
				c.Invalidate()
			})
			return
		}
		primed := &primedVideoDecoder{dec: dec}
		first, err := primed.Next()
		if err != nil {
			_ = dec.Close()
			a.post(func() {
				if seq != a.npVideoSeq {
					return
				}
				a.npVideoStarting = false
				a.npVideoMode = false
				c.Toast("视频加载失败，继续以音频播放")
				c.Invalidate()
			})
			return
		}
		primed.first = first
		primed.hasFirst = true
		primed.sent = false

		a.post(func() {
			if seq != a.npVideoSeq || !a.npVideoMode {
				_ = dec.Close()
				return
			}
			if c2 := a.pl.Current(); c2 == nil || c2.ID != curID {
				_ = dec.Close()
				return
			}
			vp, err := ui.NewVideoPlayer(primed)
			if err != nil {
				_ = dec.Close()
				a.npVideoStarting = false
				a.npVideoMode = false
				c.Toast("视频加载失败，继续以音频播放")
				c.Invalidate()
				return
			}
			a.npVideo = vp
			a.npVideoStarting = false
			if a.pl.Playing() {
				vp.Play()
			} else {
				vp.Pause()
			}
			c.Invalidate()
		})
	}()
}
