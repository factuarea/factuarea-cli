package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"math"
	"mime/multipart"
	"net/http"
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

// WithSleep inyecta la función de espera entre reintentos. Los tests la
// stubean con un no-op para que el backoff no duerma de verdad.
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

// Do ejecuta una petición. body puede ser nil. extraHeaders sobreescribe los
// por defecto (p.ej. una Idempotency-Key explícita). Reintenta 429/5xx con
// backoff respetando Retry-After.
func (c *Client) Do(ctx context.Context, method, path string, body []byte, extraHeaders map[string]string) (*Response, error) {
	url := c.baseURL + path
	idempotencyKey := extraHeaders["Idempotency-Key"]
	if idempotencyKey == "" && method == http.MethodPost {
		idempotencyKey = newIdempotencyKey()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader(body))
		if err != nil {
			return nil, err
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
		// extraHeaders se aplica al final, así que un Content-Type explícito
		// (p.ej. el boundary de multipart) gana sobre el application/json por
		// defecto. Igual con una Idempotency-Key explícita.
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		httpResp, err := c.hc.Do(req)
		if err != nil {
			lastErr = &apierr.TransportError{Err: err}
			if attempt < c.maxRetries {
				c.sleep(backoff(attempt))
				continue
			}
			return nil, lastErr
		}

		respBody, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		resp := &Response{
			StatusCode:  httpResp.StatusCode,
			Header:      httpResp.Header,
			Body:        respBody,
			ContentType: httpResp.Header.Get("Content-Type"),
			RequestID:   httpResp.Header.Get("X-Request-Id"),
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		if isRetryable(resp.StatusCode) && attempt < c.maxRetries {
			c.sleep(retryDelay(resp, attempt))
			continue
		}
		return resp, parseError(resp)
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
	// 200ms, 400ms, 800ms... Los tests inyectan WithSleep (no-op) para no esperar;
	// se puede añadir jitter real en producción sin afectar a la lógica de reintento.
	return time.Duration(math.Pow(2, float64(attempt))) * 200 * time.Millisecond
}

func retryDelay(resp *Response, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return backoff(attempt)
}

func newIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "cli_" + hex.EncodeToString(b)
}

// MultipartBody arma un cuerpo multipart/form-data real (no base64) con campos
// de texto y ficheros leídos de disco. El nombre del fichero subido es su
// basename. Devuelve el body y el Content-Type con boundary, listo para pasarse
// a Do vía extraHeaders["Content-Type"].
func MultipartBody(fields, files map[string]string) (body []byte, contentType string, err error) {
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
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// writeFormFile copia un fichero de disco al writer multipart, garantizando el
// cierre del descriptor aunque falle a media escritura.
func writeFormFile(mw *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
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
