package ytmusic

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	baseURL          = "https://music.youtube.com/youtubei/v1/"
	youtubeBaseURL   = "https://www.youtube.com/youtubei/v1/"
	clientName       = "WEB_REMIX"
	clientVersion    = "1.20261027.10.00"
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	androidMusicClientName    = "ANDROID_MUSIC"
	androidMusicClientVersion = "7.27.52"
	androidMusicUserAgent     = "com.google.android.apps.youtube.music/7.27.52 (Linux; U; Android 11) gzip"
)

// Client is the YouTube Music API client.
type Client struct {
	mu         sync.RWMutex
	httpClient *http.Client
	language   string
	region     string
	cookie     string
	sabrTokens POTProvider
	sabrDir    string
	// sabrCred is the visitor the SABR path speaks as and its PO token, one
	// mint serving many tracks; sabrCredGen names it, so that a refusal
	// invalidates the pair it was playing with and not a newer one.
	sabrCred    *sabrCredential
	sabrCredGen uint64
	// sabrMintMu holds the one mint at a time a session does.
	sabrMintMu sync.Mutex
}

// DefaultClient is the package-level client.
var DefaultClient = NewClient()

// NewClient creates a new configured YouTube Music client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		language: "zh-CN",
		region:   "US",
	}
}

// SetAuth sets the session cookie used for authenticated requests.
func (c *Client) SetAuth(cookie string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cookie = cookie
}

// SetLanguage sets the preferred language.
func (c *Client) SetLanguage(lang string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.language = lang
}

func (c *Client) post(ctx context.Context, endpoint string, body map[string]any, q url.Values) ([]byte, error) {
	return c.postWithClient(ctx, endpoint, body, q, nil, "")
}

// postWithClient performs a request with an optional client context override
// and user agent. A nil override uses the WEB_REMIX client.
func (c *Client) postWithClient(ctx context.Context, endpoint string, body map[string]any, q url.Values, clientOverride map[string]any, userAgent string) ([]byte, error) {
	sessionGen, hasSession := sessionGeneration()

	c.mu.RLock()
	lang := c.language
	reg := c.region
	cookie := c.cookie
	c.mu.RUnlock()

	if q == nil {
		q = url.Values{}
	}
	q.Set("alt", "json")

	reqURL := baseURL + endpoint + "?" + q.Encode()

	client := map[string]any{
		"clientName":    clientName,
		"clientVersion": clientVersion,
		"hl":            lang,
		"gl":            reg,
	}
	for k, v := range clientOverride {
		client[k] = v
	}

	payload := map[string]any{
		"context": map[string]any{
			"client": client,
			"user": map[string]any{
				"lockedSafetyMode": false,
			},
		},
	}
	for k, v := range body {
		payload[k] = v
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://music.youtube.com/")
	req.Header.Set("Origin", "https://music.youtube.com")

	if cookie != "" {
		cookieHeader := cookie
		sapisid := ExtractSAPISID(cookie)
		if sapisid != "" {
			req.Header.Set("Authorization", GenerateSAPISIDHash(sapisid, "https://music.youtube.com"))
			if !strings.Contains(cookie, "=") {
				cookieHeader = fmt.Sprintf("SAPISID=%s; __Secure-3PAPISID=%s", sapisid, sapisid)
			}
		}
		req.Header.Set("Cookie", cookieHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Merge refreshed cookies when the request's session generation is current.
	if setCookies := resp.Cookies(); len(setCookies) > 0 && cookie != "" {
		newCookie := mergeCookies(cookie, setCookies)
		if newCookie != cookie {
			applied := true
			if hasSession {
				applied = UpdateSessionCookie(sessionGen, newCookie) == nil
			}
			if applied {
				c.mu.Lock()
				if c.cookie == cookie {
					c.cookie = newCookie
				}
				c.mu.Unlock()
			}
		}
	}

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ytmusic api %s error HTTP %d: %s", endpoint, resp.StatusCode, string(data))
	}

	return io.ReadAll(resp.Body)
}

// RunsToString converts a YouTube runs array or map with runs to plain text.
func RunsToString(raw any) string {
	if raw == nil {
		return ""
	}
	if m, ok := raw.(map[string]any); ok {
		raw = m["runs"]
	}
	slice, ok := raw.([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, item := range slice {
		if m, ok := item.(map[string]any); ok {
			if t, ok := m["text"].(string); ok {
				b.WriteString(t)
			}
		}
	}
	return b.String()
}

// ParseThumbnails extracts thumbnail objects from raw JSON in any YouTube format.
func ParseThumbnails(raw any) []Thumbnail {
	if raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	// Check if nested in thumbnailRenderer
	if tr, ok := m["thumbnailRenderer"].(map[string]any); ok {
		m = tr
	}
	// Check if nested in musicThumbnailRenderer or croppedSquareThumbnailRenderer
	if mtr, ok := m["musicThumbnailRenderer"].(map[string]any); ok {
		m = mtr
	} else if cstr, ok := m["croppedSquareThumbnailRenderer"].(map[string]any); ok {
		m = cstr
	}
	// Check if nested in thumbnail
	if th, ok := m["thumbnail"].(map[string]any); ok {
		m = th
	}

	arr, ok := m["thumbnails"].([]any)
	if !ok {
		return nil
	}
	var out []Thumbnail
	for _, item := range arr {
		tmap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		u, _ := tmap["url"].(string)
		w, _ := tmap["width"].(float64)
		h, _ := tmap["height"].(float64)
		if u != "" {
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			}
			out = append(out, Thumbnail{
				URL:    u,
				Width:  int(w),
				Height: int(h),
			})
		}
	}
	return out
}

// BestThumbnail returns the URL of the highest resolution thumbnail.
func BestThumbnail(thumbs []Thumbnail) string {
	if len(thumbs) == 0 {
		return ""
	}
	best := thumbs[len(thumbs)-1].URL
	// Normalize protocol relative URLs
	if strings.HasPrefix(best, "//") {
		best = "https:" + best
	}
	return best
}

// ParseDurationMs parses "3:45", "1:02:15" or seconds into milliseconds.
func ParseDurationMs(text string) int64 {
	text = strings.TrimSpace(text)
	parts := strings.Split(text, ":")
	if len(parts) == 2 {
		m, merr := strconv.ParseInt(parts[0], 10, 64)
		s, serr := strconv.ParseInt(parts[1], 10, 64)
		if merr != nil || serr != nil || m < 0 || s < 0 {
			return 0
		}
		return (m*60 + s) * 1000
	} else if len(parts) == 3 {
		h, herr := strconv.ParseInt(parts[0], 10, 64)
		m, merr := strconv.ParseInt(parts[1], 10, 64)
		s, serr := strconv.ParseInt(parts[2], 10, 64)
		if herr != nil || merr != nil || serr != nil || h < 0 || m < 0 || s < 0 {
			return 0
		}
		return (h*3600 + m*60 + s) * 1000
	}
	sec, err := strconv.ParseInt(text, 10, 64)
	if err == nil && sec >= 0 {
		return sec * 1000
	}
	return 0
}

// ExtractSAPISID finds the SAPISID or __Secure-3PAPISID token from a raw cookie string.
func ExtractSAPISID(rawCookie string) string {
	rawCookie = strings.TrimPrefix(rawCookie, "Cookie:")
	rawCookie = strings.TrimPrefix(rawCookie, "cookie:")
	rawCookie = strings.TrimSpace(rawCookie)

	parts := strings.Split(rawCookie, ";")
	var sapisid string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "__Secure-3PAPISID=") {
			return strings.TrimPrefix(p, "__Secure-3PAPISID=")
		}
		if strings.HasPrefix(p, "SAPISID=") {
			sapisid = strings.TrimPrefix(p, "SAPISID=")
		}
	}
	if sapisid != "" {
		return sapisid
	}
	if len(rawCookie) > 10 && !strings.Contains(rawCookie, "=") && !strings.Contains(rawCookie, ";") {
		return rawCookie
	}
	return ""
}

// GenerateSAPISIDHash generates the SAPISIDHASH header value.
func GenerateSAPISIDHash(sapisid string, origin string) string {
	if origin == "" {
		origin = "https://music.youtube.com"
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := sha1.New()
	h.Write([]byte(ts + " " + sapisid + " " + origin))
	digest := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("SAPISIDHASH %s_%s", ts, digest)
}

func mergeCookies(existing string, setCookies []*http.Cookie) string {
	if len(setCookies) == 0 {
		return existing
	}
	cookieMap := map[string]string{}
	for _, part := range strings.Split(existing, ";") {
		part = strings.TrimSpace(part)
		if k, v, ok := strings.Cut(part, "="); ok {
			cookieMap[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, c := range setCookies {
		if c.Value != "" && c.MaxAge >= 0 {
			cookieMap[c.Name] = c.Value
		} else if c.MaxAge < 0 {
			delete(cookieMap, c.Name)
		}
	}
	var parts []string
	for k, v := range cookieMap {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, "; ")
}
