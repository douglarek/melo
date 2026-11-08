package app

import (
	"context"
	"embed"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/douglarek/melo/internal/ytmusic"
)

//go:embed pot_page.html bgutils.js
var potFS embed.FS

// PotFetchRequest asks the Pot service for an HTTP response the token page
// cannot fetch itself across origins.
type PotFetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// PotFetchResponse is the response of Pot.Fetch.
type PotFetchResponse struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// Pot is the bridge the hidden token page uses for BotGuard requests.
type Pot struct {
	client *http.Client
}

// Fetch performs an HTTP request for the token page.
func (p *Pot) Fetch(ctx context.Context, req PotFetchRequest) (PotFetchResponse, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	r, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return PotFetchResponse{}, err
	}
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", defaultPOUserAgent)
	}
	resp, err := p.client.Do(r)
	if err != nil {
		return PotFetchResponse{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return PotFetchResponse{}, err
	}
	return PotFetchResponse{
		OK:     resp.StatusCode >= 200 && resp.StatusCode < 300,
		Status: resp.StatusCode,
		Body:   string(data),
	}, nil
}

const defaultPOUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// potProvider mints visitor-bound PO tokens in a hidden window, where the
// BotGuard interpreter runs against a real DOM. The window lives only while a
// token is minted: one serves the tracks of a session, so WebKit's processes
// are gone the rest of the time, their 80 MB with them.
type potProvider struct {
	// mu guards the window against a close while it opens, and the idle timer.
	mu    sync.Mutex
	win   *mygo.Window
	ready bool
	idle  *time.Timer
	// used is when a token was last minted, for the timer that closes a
	// window that has served its mint.
	used time.Time
	// mintMu holds the one mint at a time a session does.
	mintMu sync.Mutex
}

const (
	// potIdle is how long the window stays after a mint, for a session that
	// mints again soon, as a stream refused and its retry do.
	potIdle = 30 * time.Second
	// potReadyLimit is how long the page may take to be ready.
	potReadyLimit = 5 * time.Second
	// potMintLimit is how long a mint may take.
	potMintLimit = 10 * time.Second
)

func newPOTProvider() *potProvider { return &potProvider{} }

// potPage returns the token page with the BotGuard client in it: the page
// loads with no request of its own.
func potPage() string {
	page, _ := potFS.ReadFile("pot_page.html")
	client, _ := potFS.ReadFile("bgutils.js")
	return strings.Replace(string(page), `<script src="bgutils.js"></script>`, "<script>"+string(client)+"</script>", 1)
}

// open returns the page of a window that has the token page ready, opening
// one when none is.
func (p *potProvider) open(ctx context.Context) (*mygo.Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.win != nil && p.ready {
		return p.win.Page(), nil
	}
	if p.win != nil {
		p.win.Close()
		p.win = nil
		p.ready = false
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Hidden: true,
		Width:  480,
		Height: 360,
	})
	page := win.Page()
	page.LoadHTML(potPage(), "")
	deadline := time.Now().Add(potReadyLimit)
	for {
		if err := ctx.Err(); err != nil {
			win.Close()
			return nil, err
		}
		if v, err := page.EvalContext(ctx, "document.readyState === 'complete' && window.potReady === true"); err == nil && v == true {
			break
		}
		if time.Now().After(deadline) {
			win.Close()
			return nil, errors.New("pot: page not ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.win, p.ready = win, true
	return page, nil
}

// Token mints a PO token bound to binding, opening the window it needs and
// closing it once it has served, some time after the mint.
func (p *potProvider) Token(ctx context.Context, binding string) (string, error) {
	p.mintMu.Lock()
	defer p.mintMu.Unlock()
	p.stopIdle()
	page, err := p.open(ctx)
	if err != nil {
		p.closeWindow()
		return "", err
	}
	defer p.armIdle()

	start := time.Now()
	token, err := p.mint(ctx, page, binding)
	if err == nil {
		log.Printf("pot: minted a token in %v", time.Since(start).Round(time.Millisecond))
		return token, nil
	}
	log.Printf("pot: mint failed (%v), starting the page over", err)
	// A page whose BotGuard got stuck mints after it starts over.
	_, _ = page.EvalContext(ctx, "window.resetPoMinter && window.resetPoMinter(); true")
	return p.mint(ctx, page, binding)
}

// mint asks the page for a token bound to binding.
func (p *potProvider) mint(ctx context.Context, page *mygo.Page, binding string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, potMintLimit)
	defer cancel()
	v, err := page.EvalContext(ctx, "return await window.mintPoToken("+strconv.Quote(binding)+")")
	if err != nil {
		return "", err
	}
	token, _ := v.(string)
	if token == "" {
		return "", errors.New("pot: empty token")
	}
	return token, nil
}

// armIdle closes the window once it has served nothing for potIdle.
func (p *potProvider) armIdle() {
	p.mu.Lock()
	p.used = time.Now()
	if p.idle != nil {
		p.idle.Stop()
	}
	p.idle = time.AfterFunc(potIdle, p.idleClose)
	p.mu.Unlock()
}

// idleClose closes the window if no mint has used it since it was armed: a
// mint that started meanwhile arms another.
func (p *potProvider) idleClose() {
	p.mu.Lock()
	if time.Since(p.used) < potIdle {
		p.idle.Stop()
		p.idle = time.AfterFunc(potIdle-time.Since(p.used), p.idleClose)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	p.mintMu.Lock()
	defer p.mintMu.Unlock()
	p.closeWindow()
}

func (p *potProvider) stopIdle() {
	p.mu.Lock()
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	p.mu.Unlock()
}

// closeWindow closes the window the token page ran in, and WebKit's processes
// with it, a few seconds later.
func (p *potProvider) closeWindow() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	if p.win != nil {
		log.Print("pot: closing the token window")
		p.win.Close()
		p.win = nil
	}
	p.ready = false
}

// close closes the window for good, as the app quits.
func (p *potProvider) close() {
	p.stopIdle()
	p.closeWindow()
}

var _ ytmusic.POTProvider = (*potProvider)(nil)
