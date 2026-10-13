package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

const defaultBaseURL = "https://api.factuarea.com"

type Client struct {
	baseURL    string
	apiKey     string
	hc         *http.Client
	apiVersion string
	maxRetries int
	sleep      func(time.Duration)
}

type Option func(*Client)

func WithBaseURL(u string) Option           { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }
func WithMaxRetries(n int) Option           { return func(c *Client) { c.maxRetries = n } }
func WithAPIVersion(v string) Option        { return func(c *Client) { c.apiVersion = v } }

func WithSleep(fn func(time.Duration)) Option { return func(c *Client) { c.sleep = fn } }

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		hc:         &http.Client{Timeout: 60 * time.Second},
		maxRetries: 3,
		sleep:      time.Sleep,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type Response struct {
	StatusCode  int
	Header      http.Header
	Body        []byte
	ContentType string
	RequestID   string
}

func (c *Client) Do(ctx context.Context, method, path string, body []byte, extraHeaders map[string]string) (*Response, error) {
	endpoint := c.baseURL + path
	mutating := method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
	if err := ctx.Err(); err != nil {
		return nil, &apierr.TransportError{Err: err}
	}
	idempotencyKey := ""
	keyHeaders := 0
	for name, value := range extraHeaders {
		if strings.EqualFold(name, "Idempotency-Key") {
			keyHeaders++
			idempotencyKey = value
		}
	}
	if keyHeaders > 1 {
		return nil, apierr.Usagef("Idempotency-Key debe tener una sola identidad original")
	}
	if idempotencyKey == "" && (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete) {
		idempotencyKey = NewIdempotencyKey()
	}

	// net/http follows redirects (and may replay a body on 307/308) by default.
	// Every request here keeps the original endpoint and credential boundary.
	hc := *c.hc
	hc.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	// Go's Transport may internally replay requests carrying Idempotency-Key
	// after a reused connection fails. A write uses a fresh transport connection
	// and has no GetBody replay callback, independent of this client's retry loop.
	if mutating {
		transport := hc.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if native, ok := transport.(*http.Transport); ok {
			isolated := native.Clone()
			isolated.DisableKeepAlives = true
			hc.Transport = isolated
			defer isolated.CloseIdleConnections()
		}
	}
	maxRetries := c.maxRetries
	if mutating {
		maxRetries = 0
	}
	unconfirmed := func(cause error) error {
		digest := sha256.Sum256(body)
		requestPath := path
		if parsed, err := url.Parse(path); err == nil {
			requestPath = parsed.Path
		}
		return &apierr.WriteUnconfirmed{Err: cause, Method: method, Path: requestPath, IdempotencyKey: idempotencyKey, OriginalBodySHA256: hex.EncodeToString(digest[:])}
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader(body))
		if err != nil {
			return nil, err
		}
		if mutating {
			req.GetBody = nil
			req.Close = true
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if c.apiVersion != "" {
			req.Header.Set("Factuarea-Version", c.apiVersion)
		}
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		httpResp, err := hc.Do(req)
		if err != nil {
			if mutating {
				return nil, unconfirmed(err)
			}
			if ctx.Err() != nil {
				return nil, &apierr.TransportError{Err: ctx.Err()}
			}
			lastErr = &apierr.TransportError{Err: fmt.Errorf("no se pudo completar la lectura HTTP")}
			if attempt < maxRetries {
				c.sleep(backoff(attempt))
				continue
			}
			return nil, lastErr
		}

		respBody, readErr := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		resp := &Response{
			StatusCode:  httpResp.StatusCode,
			Header:      httpResp.Header,
			Body:        respBody,
			ContentType: httpResp.Header.Get("Content-Type"),
			RequestID:   httpResp.Header.Get("X-Request-Id"),
		}
		if readErr != nil {
			if mutating {
				return resp, unconfirmed(readErr)
			}
			return resp, &apierr.TransportError{Err: fmt.Errorf("respuesta HTTP incompleta")}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		if mutating && (resp.StatusCode >= 500 || (resp.StatusCode >= 300 && resp.StatusCode < 400)) {
			return resp, unconfirmed(parseCredentialError(resp, c.apiKey))
		}
		if isRetryable(resp.StatusCode) && attempt < maxRetries {
			c.sleep(retryDelay(resp, attempt))
			continue
		}
		return resp, parseCredentialError(resp, c.apiKey)
	}
	return nil, lastErr
}

func bodyReader(b []byte) io.Reader {
	if b == nil {
		return nil
	}
	return bytes.NewReader(b)
}

func isRetryable(status int) bool {
	return status == 429 || status >= 500
}

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * 200 * time.Millisecond
}

func retryDelay(resp *Response, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			if secs >= 0 && secs <= 3600 {
				return time.Duration(secs) * time.Second
			}
		}
		if date, err := http.ParseTime(ra); err == nil {
			delay := time.Until(date)
			if delay >= 0 && delay <= time.Hour {
				return delay
			}
		}
	}
	return backoff(attempt)
}

// NewIdempotencyKey genera una clave de idempotencia estable para una sola
// mutación. Exportada para que `internal/cmd` la use cuando el spec exige la
// cabecera en una operación cuyo método no autogenera la clave aquí (ver
// `design.md` D6); `Do` la usa igual para el resto de verbos mutadores.
func NewIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "cli_" + hex.EncodeToString(b)
}

func MultipartBody(fields, files map[string]string, fileArrays ...map[string][]string) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for field, path := range files {
		if err := writeFormFile(mw, field, path); err != nil {
			return nil, "", err
		}
	}
	for _, arrays := range fileArrays {
		for field, paths := range arrays {
			for _, path := range paths {
				if err := writeFormFile(mw, field, path); err != nil {
					return nil, "", err
				}
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

func writeFormFile(mw *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return apierr.ReadFileUsagef(path, err)
	}
	defer f.Close()
	w, err := mw.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		return err
	}
	return nil
}
