package ytmusic

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func protoTestMsg(build func(*protoBuf)) []byte {
	var p protoBuf
	build(&p)
	return p.Bytes()
}

func umpPartBytes(typ int, payload []byte) []byte {
	var b bytes.Buffer
	b.Write(umpEncode(typ))
	b.Write(umpEncode(len(payload)))
	b.Write(payload)
	return b.Bytes()
}

func TestAuditGetNextWithoutShortByline(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"contents": {
				"singleColumnMusicWatchNextResultsRenderer": {
					"tabbedRenderer": {
						"watchNextTabbedResultsRenderer": {
							"tabs": [{
								"tabRenderer": {
									"title": "Up Next",
									"content": {
										"musicQueueRenderer": {
											"content": {
												"playlistPanelRenderer": {
													"contents": [{
														"playlistPanelVideoRenderer": {
															"videoId": "vid-1",
															"title": {"runs": [{"text": "No Byline Song"}]}
														}
													}]
												}
											}
										}
									}
								}
							}]
						}
					}
				}
			}
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	tracks, _, err := c.GetNext(t.Context(), "vid-0", "")
	if err != nil {
		t.Fatalf("GetNext: %v", err)
	}
	if len(tracks) != 1 || tracks[0].ID != "vid-1" || tracks[0].Title != "No Byline Song" {
		t.Fatalf("tracks = %+v", tracks)
	}
}

func TestAuditRelatedWithoutHeader(t *testing.T) {
	root := map[string]any{
		"contents": map[string]any{
			"musicCarouselShelfRenderer": map[string]any{
				"contents": []any{
					map[string]any{
						"musicResponsiveListItemRenderer": map[string]any{
							"playlistItemData": map[string]any{"videoId": "rel-1"},
							"flexColumns": []any{
								map[string]any{
									"musicResponsiveListItemFlexColumnRenderer": map[string]any{
										"text": map[string]any{"runs": []any{map[string]any{"text": "Related Song"}}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	sections := parseRelatedSections(root)
	if len(sections) != 1 || len(sections[0].Items) != 1 || sections[0].Items[0].ID != "rel-1" {
		t.Fatalf("sections = %+v", sections)
	}
}

func TestAuditParsersHandleMissingOptionalFields(t *testing.T) {
	// Search card and list item missing title/subtitle runs.
	res, err := parseSearchResponse([]byte(`{
		"contents": {
			"tabbedSearchResultsRenderer": {
				"tabs": [{
					"tabRenderer": {
						"content": {
							"sectionListRenderer": {
								"contents": [
									{"musicCardShelfRenderer": {}},
									{"musicShelfRenderer": {"contents": [{"musicResponsiveListItemRenderer": {"flexColumns": [{}]}}]}}
								]
							}
						}
					}
				}]
			}
		}
	}`))
	if err != nil || res == nil {
		t.Fatalf("parseSearchResponse = %+v, %v", res, err)
	}

	// Podcast stream missing playabilityStatus.
	if _, err := podcastStream("ep1", map[string]any{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("podcastStream err = %v, want ErrUnavailable", err)
	}

	// Charts country parser missing subheaders/options.
	countries, selected := parseChartCountries(map[string]any{
		"contents": map[string]any{
			"singleColumnBrowseResultsRenderer": map[string]any{
				"tabs": []any{map[string]any{}},
			},
		},
	})
	if len(countries) != 0 || selected != "" {
		t.Fatalf("parseChartCountries = %+v, %q", countries, selected)
	}
}

func TestAuditConcurrentCookieRefresh(t *testing.T) {
	useTempSessionFile(t)

	c := NewClient()
	var wg sync.WaitGroup
	wg.Add(2)
	bothEntered := make(chan struct{})
	var enteredMu sync.Mutex
	entered := 0

	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		enteredMu.Lock()
		entered++
		n := entered
		if entered == 2 {
			close(bothEntered)
		}
		enteredMu.Unlock()
		<-bothEntered

		header := make(http.Header)
		if n == 1 {
			header.Add("Set-Cookie", "SID=fresh-sid; Path=/")
		} else {
			header.Add("Set-Cookie", "PREF=fresh-pref; Path=/")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    req,
		}, nil
	})}

	if err := SaveSession(&SessionData{Cookie: "SID=old-sid; PREF=old-pref"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	c.SetAuth("SID=old-sid; PREF=old-pref")

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, err := c.post(t.Context(), "test", nil, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("post: %v", err)
		}
	}

	snap, _, ok := SessionSnapshot()
	if !ok {
		t.Fatal("missing session snapshot")
	}
	want := "PREF=fresh-pref; SID=fresh-sid"
	if snap.Cookie != want {
		t.Fatalf("persisted cookie = %q, want %q", snap.Cookie, want)
	}
	if got := c.Cookie(); got != want {
		t.Fatalf("client cookie = %q, want %q", got, want)
	}
}

func TestAuditCookieRefreshAfterClearOrSwitchSession(t *testing.T) {
	t.Run("after clear", func(t *testing.T) {
		useTempSessionFile(t)
		c := NewClient()
		started := make(chan struct{})
		release := make(chan struct{})
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(started)
			<-release
			return cookieResponse(req), nil
		})}

		if err := SaveSession(&SessionData{Cookie: "SID=account-A"}); err != nil {
			t.Fatal(err)
		}
		c.SetAuth("SID=account-A")

		done := make(chan error, 1)
		go func() {
			_, err := c.post(t.Context(), "test", nil, nil)
			done <- err
		}()
		<-started

		if err := ClearSession(); err != nil {
			t.Fatal(err)
		}
		c.SetAuth("")
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		if snap, _, ok := SessionSnapshot(); ok {
			t.Fatalf("cleared session revived: %+v", snap)
		}
		if got := c.Cookie(); got != "" {
			t.Fatalf("logged-out client re-authenticated: %q", got)
		}
	})

	t.Run("after switch", func(t *testing.T) {
		useTempSessionFile(t)
		c := NewClient()
		started := make(chan struct{})
		release := make(chan struct{})
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(started)
			<-release
			return cookieResponse(req), nil
		})}

		if err := SaveSession(&SessionData{Cookie: "SID=account-A"}); err != nil {
			t.Fatal(err)
		}
		c.SetAuth("SID=account-A")

		done := make(chan error, 1)
		go func() {
			_, err := c.post(t.Context(), "test", nil, nil)
			done <- err
		}()
		<-started

		if err := SaveSession(&SessionData{Cookie: "SID=account-B"}); err != nil {
			t.Fatal(err)
		}
		c.SetAuth("SID=account-B")
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		snap, _, ok := SessionSnapshot()
		if !ok || snap.Cookie != "SID=account-B" {
			t.Fatalf("session = %+v, want account-B", snap)
		}
		if got := c.Cookie(); got != "SID=account-B" {
			t.Fatalf("client cookie = %q, want account-B", got)
		}
	})
}

func TestAuditPendingLoginCookieRefreshIsolated(t *testing.T) {
	t.Run("saved B supersedes pending A", func(t *testing.T) {
		useTempSessionFile(t)
		c := NewClient()
		started := make(chan struct{})
		release := make(chan struct{})
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(started)
			<-release
			header := make(http.Header)
			header.Add("Set-Cookie", "SID=refreshed-account-A; Path=/")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    req,
			}, nil
		})}

		// No saved session yet, only pending SetAuth for A.
		c.SetAuth("SID=pending-A")

		done := make(chan error, 1)
		go func() {
			_, err := c.post(t.Context(), "account/account_menu", nil, nil)
			done <- err
		}()
		<-started

		if err := SaveSession(&SessionData{Cookie: "SID=account-B"}); err != nil {
			t.Fatal(err)
		}
		c.SetAuth("SID=account-B")
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		if got := c.Cookie(); got != "SID=account-B" {
			t.Fatalf("client cookie = %q, want SID=account-B", got)
		}
	})

	t.Run("pending B supersedes pending A without saved session", func(t *testing.T) {
		useTempSessionFile(t)
		c := NewClient()
		started := make(chan struct{})
		release := make(chan struct{})
		c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(started)
			<-release
			header := make(http.Header)
			header.Add("Set-Cookie", "SID=refreshed-account-A; Path=/")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    req,
			}, nil
		})}

		c.SetAuth("SID=pending-A")

		done := make(chan error, 1)
		go func() {
			_, err := c.post(t.Context(), "account/account_menu", nil, nil)
			done <- err
		}()
		<-started

		c.SetAuth("SID=pending-B")
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}

		if got := c.Cookie(); got != "SID=pending-B" {
			t.Fatalf("client cookie = %q, want SID=pending-B", got)
		}
	})
}

func TestAuditProtoOversizedLength(t *testing.T) {
	// Field 1, wire type 2 (0x0a), varint length = math.MaxUint64.
	malformed := []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0x00}
	if _, err := protoParse(malformed); err == nil {
		t.Fatal("protoParse accepted overflowing uint64 length")
	}

	// Truncated fixed64 (wire type 1) and fixed32 (wire type 5).
	if _, err := protoParse([]byte{(1 << 3) | 1, 1, 2}); err == nil {
		t.Fatal("protoParse accepted truncated fixed64")
	}
	if _, err := protoParse([]byte{(1 << 3) | 5, 1, 2}); err == nil {
		t.Fatal("protoParse accepted truncated fixed32")
	}
}

func makeSabrHeaderPart(headerID, seq int, init bool, offset, content int64) []byte {
	payload := protoTestMsg(func(p *protoBuf) {
		p.uint(1, uint64(headerID))
		p.uint(6, uint64(offset))
		if init {
			p.uint(8, 1)
		}
		if seq > 0 {
			p.uint(9, uint64(seq))
		}
		p.uint(14, uint64(content))
		if !init {
			tr := protoTestMsg(func(tp *protoBuf) {
				tp.uint(1, uint64((seq-1)*1000))
				tp.uint(2, 1000)
				tp.uint(3, 1000)
			})
			p.bytes(15, tr)
		}
	})
	return umpPartBytes(umpMediaHeader, payload)
}

func makeSabrMediaPart(headerID int, data []byte) []byte {
	payload := append(umpEncode(headerID), data...)
	return umpPartBytes(umpMedia, payload)
}

func makeSabrFormatInitPart(endSeg uint64) []byte {
	payload := protoTestMsg(func(p *protoBuf) {
		p.uint(4, endSeg)
	})
	return umpPartBytes(umpFormatInit, payload)
}

func TestAuditSabrTruncatedSegmentRejected(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "truncated.m4a")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		body.Write(makeSabrFormatInitPart(1))
		body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
		body.Write(makeSabrMediaPart(1, []byte{1, 2, 3, 4}))
		// Declare 4 bytes for seq 1, send only 2 bytes.
		body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
		body.Write(makeSabrMediaPart(2, []byte{5, 6}))
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{
		VideoID: "truncated",
		URL:     srv.URL,
		Format:  SabrFormat{Itag: 140, ContentLength: 8},
	}
	token := base64.StdEncoding.EncodeToString([]byte("tok"))
	if err := c.DownloadSabr(t.Context(), stream, token, dest); err == nil {
		t.Fatal("DownloadSabr accepted a truncated segment")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("incomplete file was published to %s", dest)
	}
	if _, err := os.Stat(dest + ".ok"); !os.IsNotExist(err) {
		t.Fatalf("marker was written for truncated download")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("temp files leaked: %v", entries)
	}
}

func TestAuditSabrRetransmittedSegmentIdempotent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "retransmit.m4a")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body bytes.Buffer
		body.Write(makeSabrFormatInitPart(2))
		if calls == 1 {
			body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
			body.Write(makeSabrMediaPart(1, []byte("INIT")))
			body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
			body.Write(makeSabrMediaPart(2, []byte("SEG1")))
		} else {
			// Retransmit seq 1 identically and deliver seq 2.
			body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
			body.Write(makeSabrMediaPart(2, []byte("SEG1")))
			body.Write(makeSabrHeaderPart(3, 2, false, 8, 4))
			body.Write(makeSabrMediaPart(3, []byte("SEG2")))
		}
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{
		VideoID: "retransmit",
		URL:     srv.URL,
		Format:  SabrFormat{Itag: 140, ContentLength: 12},
	}
	token := base64.StdEncoding.EncodeToString([]byte("tok"))
	if err := c.DownloadSabr(t.Context(), stream, token, dest); err != nil {
		t.Fatalf("DownloadSabr failed on segment retransmission: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "INITSEG1SEG2" {
		t.Fatalf("dest content = %q (%v), want INITSEG1SEG2", got, err)
	}
	if _, ok := drainedFile("retransmit", dest); !ok {
		t.Fatal("published file did not pass drainedFile")
	}
}

func TestAuditSabrPartialLastSegmentAcrossResponses(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "partial-last.m4a")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body bytes.Buffer
		body.Write(makeSabrFormatInitPart(2))
		if calls == 1 {
			body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
			body.Write(makeSabrMediaPart(1, []byte("INIT")))
			body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
			body.Write(makeSabrMediaPart(2, []byte("SEG1")))
			// Last segment arrives partially (2 of 4 bytes).
			body.Write(makeSabrHeaderPart(3, 2, false, 8, 4))
			body.Write(makeSabrMediaPart(3, []byte("SE")))
		} else {
			// Second response supplies remaining 2 bytes of seq 2.
			body.Write(makeSabrHeaderPart(3, 2, false, 10, 2))
			body.Write(makeSabrMediaPart(3, []byte("G2")))
		}
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{
		VideoID: "partial-last",
		URL:     srv.URL,
		Format:  SabrFormat{Itag: 140, ContentLength: 12},
	}
	token := base64.StdEncoding.EncodeToString([]byte("tok"))
	if err := c.DownloadSabr(t.Context(), stream, token, dest); err != nil {
		t.Fatalf("DownloadSabr failed on split last segment: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "INITSEG1SEG2" {
		t.Fatalf("dest content = %q (%v), want INITSEG1SEG2", got, err)
	}
}

func TestAuditSabrRejectsNonPositiveEndSeg(t *testing.T) {
	for _, tc := range []struct {
		name   string
		endSeg uint64
	}{
		{"zero", 0},
		{"negative_overflow", math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "bad-endseg.m4a")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body bytes.Buffer
				body.Write(makeSabrFormatInitPart(tc.endSeg))
				body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
				body.Write(makeSabrMediaPart(1, []byte("INIT")))
				body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
				body.Write(makeSabrMediaPart(2, []byte("SEG1")))
				_, _ = w.Write(body.Bytes())
			}))
			defer srv.Close()

			c := NewClient()
			stream := &SabrStream{
				VideoID: "bad-endseg",
				URL:     srv.URL,
				Format:  SabrFormat{Itag: 140, ContentLength: 8},
			}
			token := base64.StdEncoding.EncodeToString([]byte("tok"))
			if err := c.DownloadSabr(t.Context(), stream, token, dest); err == nil {
				t.Fatalf("DownloadSabr accepted endSeg=%d", tc.endSeg)
			}
			if _, err := os.Stat(dest + ".ok"); !os.IsNotExist(err) {
				t.Fatal("marker written for non-positive endSeg")
			}
		})
	}
}

func TestAuditSabrConcurrentLoadsIsolated(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "shared-id.m4a")

	reqCount := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqCount++
		n := reqCount
		mu.Unlock()

		var body bytes.Buffer
		body.Write(makeSabrFormatInitPart(1))
		body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
		body.Write(makeSabrMediaPart(1, []byte("INIT")))
		if n > 1 {
			body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
			body.Write(makeSabrMediaPart(2, []byte("GOOD")))
		}
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{
		VideoID: "shared-id",
		URL:     srv.URL,
		Format:  SabrFormat{Itag: 140, ContentLength: 8},
	}
	token := base64.StdEncoding.EncodeToString([]byte("tok"))

	ctx1, cancel1 := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel1()
	oldSess, err := newSabrSession(dest, 8)
	if err != nil {
		t.Fatal(err)
	}
	if oldSess.tempPath == dest {
		t.Fatalf("tempPath %q equals dest %q", oldSess.tempPath, dest)
	}
	// Second load completes and publishes dest + dest.ok.
	sess2, err := c.startSabrSession(t.Context(), stream, token, dest)
	if err != nil {
		t.Fatalf("second session: %v", err)
	}
	if err := sess2.Wait(t.Context()); err != nil {
		t.Fatalf("second session wait: %v", err)
	}
	_ = sess2.Close()

	// Closing stale first session must NOT remove published dest or dest.ok.
	cancel1()
	_ = ctx1
	_ = oldSess.Close()

	if _, ok := drainedFile("shared-id", dest); !ok {
		t.Fatal("stale Close removed published dest or marker")
	}
}

func TestAuditSabrReadDuringPublishAndClose(t *testing.T) {
	t.Run("continuous ReadAt across publish", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "publish-read.m4a")
		releaseSeg2 := make(chan struct{})
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var body bytes.Buffer
			body.Write(makeSabrFormatInitPart(2))
			if calls == 1 {
				body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
				body.Write(makeSabrMediaPart(1, []byte("INIT")))
				body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
				body.Write(makeSabrMediaPart(2, []byte("SEG1")))
			} else {
				<-releaseSeg2
				body.Write(makeSabrHeaderPart(3, 2, false, 8, 4))
				body.Write(makeSabrMediaPart(3, []byte("SEG2")))
			}
			_, _ = w.Write(body.Bytes())
		}))
		defer srv.Close()

		c := NewClient()
		stream := &SabrStream{
			VideoID: "publish-read",
			URL:     srv.URL,
			Format:  SabrFormat{Itag: 140, ContentLength: 12},
		}
		token := base64.StdEncoding.EncodeToString([]byte("tok"))
		sess, err := c.startSabrSession(t.Context(), stream, token, dest)
		if err != nil {
			t.Fatalf("startSabrSession: %v", err)
		}
		defer sess.Close()

		// Hammer ReadAt on already-arrived bytes while publish runs.
		readErr := make(chan error, 1)
		stopReads := make(chan struct{})
		go func() {
			buf := make([]byte, 8)
			for {
				select {
				case <-stopReads:
					readErr <- nil
					return
				default:
					n, err := sess.ReadAt(buf, 0)
					if err != nil || n != 8 || string(buf) != "INITSEG1" {
						readErr <- fmt.Errorf("ReadAt = %d, %q, %v", n, buf[:n], err)
						return
					}
				}
			}
		}()

		time.Sleep(30 * time.Millisecond)
		close(releaseSeg2)
		if err := sess.Wait(t.Context()); err != nil {
			close(stopReads)
			t.Fatalf("Wait: %v", err)
		}
		// Read the final bytes after publish too.
		tail := make([]byte, 4)
		if n, err := sess.ReadAt(tail, 8); err != nil || n != 4 || string(tail) != "SEG2" {
			close(stopReads)
			t.Fatalf("ReadAt tail = %d, %q, %v", n, tail[:n], err)
		}
		close(stopReads)
		if err := <-readErr; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("publish failure falls back or errors cleanly", func(t *testing.T) {
		dir := t.TempDir()
		// Make dest a directory so os.Rename fails, while tempPath stays readable.
		destDir := filepath.Join(dir, "blocked.m4a")
		if err := os.Mkdir(destDir, 0o700); err != nil {
			t.Fatal(err)
		}
		sess, err := newSabrSession(destDir, 4)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		if _, err := sess.writeAt([]byte("DATA"), 0); err != nil {
			t.Fatal(err)
		}
		if err := sess.publish("4"); err != nil {
			t.Fatalf("publish with temp fallback returned error: %v", err)
		}
		sess.mu.Lock()
		cacheErr := sess.cacheErr
		sess.mu.Unlock()
		if cacheErr == nil {
			t.Fatal("expected cacheErr on publish fallback")
		}
		buf := make([]byte, 4)
		if n, err := sess.ReadAt(buf, 0); err != nil || n != 4 || string(buf) != "DATA" {
			t.Fatalf("ReadAt after fallback publish = %d, %q, %v", n, buf[:n], err)
		}

		// If tempPath is also missing, publish must return error (not nil with file==nil).
		sess2, err := newSabrSession(destDir, 4)
		if err != nil {
			t.Fatal(err)
		}
		defer sess2.Close()
		if _, err := sess2.writeAt([]byte("DATA"), 0); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(sess2.tempPath)
		if err := sess2.publish("4"); err == nil {
			t.Fatal("publish succeeded even though neither temp nor dest could be opened")
		}
		if sess2.file != nil {
			t.Fatal("failed publish left non-nil file handle")
		}
	})
}

func TestAuditBrowsePlaylistUnknownTotal(t *testing.T) {
	c := NewClient()
	c.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{
			"header": {"musicResponsiveHeaderRenderer": {"title": {"runs": [{"text": "Liked"}]}}},
			"contents": {
				"singleColumnBrowseResultsRenderer": {
					"tabs": [{
						"tabRenderer": {
							"content": {
								"sectionListRenderer": {
									"contents": [{
										"musicPlaylistShelfRenderer": {
											"contents": [{
												"musicResponsiveListItemRenderer": {
													"playlistItemData": {"videoId": "t1"},
													"flexColumns": [{
														"musicResponsiveListItemFlexColumnRenderer": {
															"text": {"runs": [{"text": "Song 1"}]}
														}
													}]
												}
											}],
											"continuations": [{
												"nextContinuationData": {"continuation": "page2"}
											}]
										}
									}]
								}
							}
						}
					}]
				}
			}
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	pl, err := c.BrowsePlaylist(t.Context(), "LM")
	if err != nil {
		t.Fatalf("BrowsePlaylist: %v", err)
	}
	if pl.Continuation != "page2" {
		t.Fatalf("Continuation = %q, want page2", pl.Continuation)
	}
	if pl.TrackCount != 0 {
		t.Fatalf("TrackCount = %d, want 0 (unknown) when continuation is non-empty", pl.TrackCount)
	}
}

func TestAuditLoadSessionStaleDoesNotOverwriteNewerOwner(t *testing.T) {
	subtests := []struct {
		name       string
		action     func(t *testing.T)
		wantSess   string
		wantClient string
		wantLoad   string
	}{
		{
			name: "SaveSession",
			action: func(t *testing.T) {
				if err := SaveSession(&SessionData{Cookie: "SID=B", Profile: &Profile{Name: "User B"}}); err != nil {
					t.Fatalf("SaveSession(B): %v", err)
				}
			},
			wantSess:   "SID=B",
			wantClient: "SID=B",
			wantLoad:   "SID=B",
		},
		{
			name: "SetAuth",
			action: func(t *testing.T) {
				DefaultClient.SetAuth("SID=B")
			},
			wantSess:   "",
			wantClient: "SID=B",
		},
		{
			name: "ClearSession",
			action: func(t *testing.T) {
				if err := ClearSession(); err != nil {
					t.Fatalf("ClearSession: %v", err)
				}
			},
			wantSess:   "",
			wantClient: "",
		},
	}

	for _, tc := range subtests {
		t.Run(tc.name, func(t *testing.T) {
			useTempSessionFile(t)
			if err := writeSession(&SessionData{Cookie: "SID=A", Profile: &Profile{Name: "User A"}}); err != nil {
				t.Fatalf("writeSession(A): %v", err)
			}

			origGet := sessionSecretGet
			entered := make(chan struct{})
			release := make(chan struct{})
			var hooked atomic.Bool
			hooked.Store(true)
			sessionSecretGet = func() ([]byte, error) {
				if hooked.CompareAndSwap(true, false) {
					close(entered)
					<-release
				}
				return origGet()
			}

			type loadRes struct {
				sess *SessionData
				err  error
			}
			done := make(chan loadRes, 1)
			go func() {
				s, err := LoadSession()
				done <- loadRes{sess: s, err: err}
			}()

			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("LoadSession did not reach sessionSecretGet")
			}

			tc.action(t)
			close(release)

			res := <-done
			if !errors.Is(res.err, ErrSessionStale) || res.sess != nil {
				t.Fatalf("LoadSession = (%+v, %v), want (nil, ErrSessionStale)", res.sess, res.err)
			}

			cur := CurrentSession()
			if tc.wantSess == "" {
				if cur != nil {
					t.Fatalf("CurrentSession = %+v, want nil", cur)
				}
			} else if cur == nil || cur.Cookie != tc.wantSess {
				t.Fatalf("CurrentSession = %+v, want cookie %q", cur, tc.wantSess)
			}

			if got := DefaultClient.Cookie(); got != tc.wantClient {
				t.Fatalf("DefaultClient.Cookie() = %q, want %q", got, tc.wantClient)
			}

			if tc.wantLoad != "" {
				reloaded, err := LoadSession()
				if err != nil || reloaded.Cookie != tc.wantLoad {
					t.Fatalf("subsequent LoadSession = (%+v, %v), want %q", reloaded, err, tc.wantLoad)
				}
			} else if tc.name == "ClearSession" {
				if reloaded, err := LoadSession(); err == nil {
					t.Fatalf("LoadSession after ClearSession succeeded: %+v", reloaded)
				}
			}
		})
	}

	t.Run("legacy migration skipped when stale", func(t *testing.T) {
		secret := useTempSessionFile(t)
		*secret = nil
		if err := writeSessionBytes([]byte(`{"cookie":"SID=legacy-A","profile":{"name":"Legacy A"}}`)); err != nil {
			t.Fatal(err)
		}

		origFile := sessionFile
		entered := make(chan struct{})
		release := make(chan struct{})
		var hooked atomic.Bool
		hooked.Store(true)
		sessionFile = func() (string, error) {
			if hooked.CompareAndSwap(true, false) {
				close(entered)
				<-release
			}
			return origFile()
		}

		type loadRes struct {
			sess *SessionData
			err  error
		}
		done := make(chan loadRes, 1)
		go func() {
			s, err := LoadSession()
			done <- loadRes{sess: s, err: err}
		}()

		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("LoadSession did not reach sessionFile")
		}

		if err := SaveSession(&SessionData{Cookie: "SID=B", Profile: &Profile{Name: "User B"}}); err != nil {
			close(release)
			t.Fatalf("SaveSession(B): %v", err)
		}
		close(release)

		res := <-done
		if !errors.Is(res.err, ErrSessionStale) || res.sess != nil {
			t.Fatalf("LoadSession = (%+v, %v), want (nil, ErrSessionStale)", res.sess, res.err)
		}
		if cur := CurrentSession(); cur == nil || cur.Cookie != "SID=B" {
			t.Fatalf("CurrentSession = %+v, want SID=B", cur)
		}
		if DefaultClient.Cookie() != "SID=B" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=B", DefaultClient.Cookie())
		}
		if string(*secret) != "SID=B" {
			t.Fatalf("legacy migration overwrote secret store with %q", *secret)
		}
	})
}

func TestAuditClearSessionHoldsLockAcrossPersistenceDeletes(t *testing.T) {
	t.Run("holds sessionMu through delete and serializes SaveSession", func(t *testing.T) {
		useTempSessionFile(t)
		if err := SaveSession(&SessionData{Cookie: "SID=A", Profile: &Profile{Name: "A"}}); err != nil {
			t.Fatal(err)
		}

		origDel := sessionSecretDelete
		origFile := sessionFile
		var inClear atomic.Bool
		var delUnlocked, fileUnlocked atomic.Bool
		delEntered := make(chan struct{})
		releaseDel := make(chan struct{})

		sessionSecretDelete = func() error {
			if inClear.Load() {
				if sessionMu.TryLock() {
					delUnlocked.Store(true)
					sessionMu.Unlock()
				}
				close(delEntered)
				<-releaseDel
			}
			return origDel()
		}
		sessionFile = func() (string, error) {
			if inClear.Load() {
				if sessionMu.TryLock() {
					fileUnlocked.Store(true)
					sessionMu.Unlock()
				}
			}
			return origFile()
		}

		clearDone := make(chan error, 1)
		inClear.Store(true)
		go func() {
			clearDone <- ClearSession()
		}()

		select {
		case <-delEntered:
		case <-time.After(2 * time.Second):
			close(releaseDel)
			t.Fatal("ClearSession did not call sessionSecretDelete")
		}

		saveDone := make(chan error, 1)
		go func() {
			saveDone <- SaveSession(&SessionData{Cookie: "SID=B", Profile: &Profile{Name: "B"}})
		}()

		select {
		case err := <-saveDone:
			close(releaseDel)
			t.Fatalf("SaveSession completed (%v) while ClearSession held sessionMu", err)
		case <-time.After(40 * time.Millisecond):
		}

		close(releaseDel)
		if err := <-clearDone; err != nil {
			t.Fatalf("ClearSession: %v", err)
		}
		inClear.Store(false)

		if err := <-saveDone; err != nil {
			t.Fatalf("SaveSession(B): %v", err)
		}
		if delUnlocked.Load() {
			t.Fatal("sessionMu was not held during sessionSecretDelete")
		}
		if fileUnlocked.Load() {
			t.Fatal("sessionMu was not held during deleteSessionBytes")
		}

		if cur := CurrentSession(); cur == nil || cur.Cookie != "SID=B" {
			t.Fatalf("CurrentSession = %+v, want SID=B", cur)
		}
		if got := DefaultClient.Cookie(); got != "SID=B" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=B", got)
		}
		reloaded, err := LoadSession()
		if err != nil || reloaded.Cookie != "SID=B" {
			t.Fatalf("LoadSession = (%+v, %v), want SID=B", reloaded, err)
		}
	})

	t.Run("save and reload B after logout", func(t *testing.T) {
		useTempSessionFile(t)
		if err := SaveSession(&SessionData{Cookie: "SID=A"}); err != nil {
			t.Fatal(err)
		}
		if err := ClearSession(); err != nil {
			t.Fatal(err)
		}
		if err := SaveSession(&SessionData{Cookie: "SID=B", Profile: &Profile{Name: "User B"}}); err != nil {
			t.Fatalf("SaveSession(B): %v", err)
		}

		sessionMu.Lock()
		currentSession = nil
		sessionMu.Unlock()
		DefaultClient.SetAuth("")

		loaded, err := LoadSession()
		if err != nil {
			t.Fatalf("LoadSession after logout+SaveSession(B): %v", err)
		}
		if loaded.Cookie != "SID=B" || loaded.Profile == nil || loaded.Profile.Name != "User B" {
			t.Fatalf("loaded = %+v, want SID=B / User B", loaded)
		}
		if got := DefaultClient.Cookie(); got != "SID=B" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=B", got)
		}
	})
}

func TestAuditSaveSessionSyncsClientAndPreservesPendingRefresh(t *testing.T) {
	t.Run("updates DefaultClient cookie", func(t *testing.T) {
		useTempSessionFile(t)
		if err := SaveSession(&SessionData{Cookie: "SID=first"}); err != nil {
			t.Fatal(err)
		}
		if got := DefaultClient.Cookie(); got != "SID=first" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=first", got)
		}
		if err := SaveSession(&SessionData{Cookie: "SID=second"}); err != nil {
			t.Fatal(err)
		}
		if got := DefaultClient.Cookie(); got != "SID=second" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=second", got)
		}
	})

	t.Run("preserves pending-login cookie refresh", func(t *testing.T) {
		useTempSessionFile(t)
		oldClient := DefaultClient.httpClient
		t.Cleanup(func() {
			DefaultClient.mu.Lock()
			DefaultClient.httpClient = oldClient
			DefaultClient.mu.Unlock()
		})
		DefaultClient.mu.Lock()
		DefaultClient.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Add("Set-Cookie", "PREF=fresh-pref; Path=/")
			header.Add("Set-Cookie", "SID=refreshed-A; Path=/")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    req,
			}, nil
		})}
		DefaultClient.mu.Unlock()

		DefaultClient.SetAuth("SID=pending-A")
		if _, err := DefaultClient.post(t.Context(), "account/account_menu", nil, nil); err != nil {
			t.Fatalf("post: %v", err)
		}
		want := "PREF=fresh-pref; SID=refreshed-A"
		if got := DefaultClient.Cookie(); got != want {
			t.Fatalf("pre-save client cookie = %q, want %q", got, want)
		}

		// Caller passes original pending cookie; SaveSession must preserve the refreshed value.
		if err := SaveSession(&SessionData{Cookie: "SID=pending-A", Profile: &Profile{Name: "A"}}); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
		if snap, _, ok := SessionSnapshot(); !ok || snap.Cookie != want {
			t.Fatalf("SessionSnapshot = %+v (ok=%v), want %q", snap, ok, want)
		}
		if got := DefaultClient.Cookie(); got != want {
			t.Fatalf("post-save DefaultClient.Cookie() = %q, want %q", got, want)
		}

		sessionMu.Lock()
		currentSession = nil
		sessionMu.Unlock()
		DefaultClient.SetAuth("")
		loaded, err := LoadSession()
		if err != nil || loaded.Cookie != want {
			t.Fatalf("reloaded = (%+v, %v), want %q", loaded, err, want)
		}
	})

	t.Run("new owner does not inherit previous pending refresh", func(t *testing.T) {
		useTempSessionFile(t)
		oldClient := DefaultClient.httpClient
		t.Cleanup(func() {
			DefaultClient.mu.Lock()
			DefaultClient.httpClient = oldClient
			DefaultClient.mu.Unlock()
		})
		DefaultClient.mu.Lock()
		DefaultClient.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return cookieResponse(req), nil
		})}
		DefaultClient.mu.Unlock()

		DefaultClient.SetAuth("SID=pending-A")
		if _, err := DefaultClient.post(t.Context(), "account/account_menu", nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := SaveSession(&SessionData{Cookie: "SID=owner-B"}); err != nil {
			t.Fatal(err)
		}
		if snap, _, ok := SessionSnapshot(); !ok || snap.Cookie != "SID=owner-B" {
			t.Fatalf("SessionSnapshot = %+v, want SID=owner-B", snap)
		}
		if got := DefaultClient.Cookie(); got != "SID=owner-B" {
			t.Fatalf("DefaultClient.Cookie() = %q, want SID=owner-B", got)
		}
	})
}

func TestAuditSabrDownloadReturnsErrorWhenDestUnpublishable(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "blocked.m4a")
	if err := os.Mkdir(destDir, 0o700); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		body.Write(makeSabrFormatInitPart(1))
		body.Write(makeSabrHeaderPart(1, 0, true, 0, 4))
		body.Write(makeSabrMediaPart(1, []byte("INIT")))
		body.Write(makeSabrHeaderPart(2, 1, false, 4, 4))
		body.Write(makeSabrMediaPart(2, []byte("SEG1")))
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	c := NewClient()
	stream := &SabrStream{
		VideoID: "blocked",
		URL:     srv.URL,
		Format:  SabrFormat{Itag: 140, ContentLength: 8},
	}
	token := base64.StdEncoding.EncodeToString([]byte("tok"))

	if err := c.DownloadSabr(t.Context(), stream, token, destDir); err == nil {
		t.Fatal("DownloadSabr returned nil even though dest could not be published")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "blocked.m4a" {
		t.Fatalf("DownloadSabr leaked files: %v", entries)
	}

	sess, err := c.startSabrSession(t.Context(), stream, token, destDir)
	if err != nil {
		t.Fatalf("startSabrSession: %v", err)
	}
	if err := sess.Wait(t.Context()); err != nil {
		_ = sess.Close()
		t.Fatalf("Wait failed despite reopened temp file: %v", err)
	}
	sess.mu.Lock()
	cacheErr := sess.cacheErr
	sess.mu.Unlock()
	if cacheErr == nil {
		_ = sess.Close()
		t.Fatal("expected cacheErr to be recorded when rename failed")
	}

	buf := make([]byte, 8)
	if n, err := sess.ReadAt(buf, 0); err != nil || n != 8 || string(buf) != "INITSEG1" {
		_ = sess.Close()
		t.Fatalf("ReadAt = %d, %q, %v; want 8, INITSEG1, nil", n, buf[:n], err)
	}
	tempPath := sess.tempPath
	if _, err := os.Stat(tempPath); err != nil {
		_ = sess.Close()
		t.Fatalf("tempPath missing before Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("Close did not remove unpublished temp file %s: %v", tempPath, err)
	}
}
