package app

import (
	"context"
	"embed"
	"errors"
	"io"
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
// BotGuard interpreter runs against a real DOM.
type potProvider struct {
	mu    sync.Mutex
	win   *mygo.Window
	ready bool
}

func newPOTProvider() *potProvider { return &potProvider{} }

func (p *potProvider) start() {
	_ = mygo.Protocol.HandleFunc("pot", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "pot_page.html"
		}
		data, err := potFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(name, ".html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		_, _ = w.Write(data)
	})
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Hidden: true,
		Width:  480,
		Height: 360,
		URL:    "pot://localhost/",
	})
}

func (p *potProvider) waitReady(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready {
		return nil
	}
	if p.win == nil || p.win.Page() == nil {
		return errors.New("pot: page not started")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		v, err := p.win.Page().EvalContext(ctx, "document.readyState === 'complete' && window.potReady === true")
		if err == nil && v == true {
			p.ready = true
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("pot: page not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Token mints a PO token bound to binding, a visitor data or a video id.
func (p *potProvider) Token(ctx context.Context, binding string) (string, error) {
	if err := p.waitReady(ctx); err != nil {
		return "", err
	}
	page := p.win.Page()
	code := "return await window.mintPoToken(" + strconv.Quote(binding) + ")"
	v, err := page.EvalContext(ctx, code)
	if err != nil || v == nil {
		_, _ = page.EvalContext(ctx, "window.resetPoMinter && window.resetPoMinter(); true")
		v, err = page.EvalContext(ctx, code)
	}
	if err != nil {
		return "", err
	}
	token, _ := v.(string)
	if token == "" {
		return "", errors.New("pot: empty token")
	}
	return token, nil
}

func (p *potProvider) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.win != nil {
		p.win.Close()
		p.win = nil
	}
	p.ready = false
}

var _ ytmusic.POTProvider = (*potProvider)(nil)
