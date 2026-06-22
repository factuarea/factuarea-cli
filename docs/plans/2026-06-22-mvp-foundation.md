# Factuarea CLI — Plan 1: Foundation / walking skeleton

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un binario `factuarea` que autentica contra la API pública v1 y ejecuta llamadas end-to-end (`factuarea api get /v1/account`), con runtime fino (auth, errores→exit codes, retries, idempotencia), config/profiles con keyring, output humano/JSON/plain y guards fiscales (`--live`, confirmación tipada).

**Architecture:** Go + Cobra. `internal/apierr` (modelo de error del backend) + `internal/exit` (códigos semánticos) + `internal/client` (HTTP fino) + `internal/config` (profiles, resolución de credenciales, keyring con fallback a TOML) + `internal/output` (formato y render) + `internal/safety` (guards) + `internal/cmd` (comandos a mano). El runtime replica el contrato de los SDKs oficiales sin reusar su código.

**Tech Stack:** Go 1.23+, `github.com/spf13/cobra`, `github.com/pelletier/go-toml/v2`, `github.com/zalando/go-keyring`, stdlib `net/http`/`net/http/httptest`/`testing`.

## Global Constraints

- Módulo Go: `github.com/factuarea/factuarea-cli`. Binario: `factuarea`.
- Base URL default: `https://api.factuarea.com`, prefijo de ruta `/v1`. Auth: `Authorization: Bearer <key>`.
- Prefijos de key: `fact_test_` / `fact_live_` + 24 alfanuméricos. El **prefijo es la única fuente de verdad del entorno** (test/live).
- Sobre de error del backend: `{ "error": { type, code, message, subcode, param, doc_url, request_id } }`. 9 `type`: `invalid_request_error`, `authentication_error`, `authorization_error`, `not_found_error`, `rate_limit_error`, `idempotency_error`, `conflict_error`, `api_error`, `service_unavailable_error`.
- Exit codes: 0 ok · 1 bug del CLI · 2 uso/guard local · 3 auth · 4 perm/plan/scope · 5 validación · 6 not-found · 7 rate-limit · 8 conflicto/idempotencia · 9 server/503 · 10 red/timeout.
- **NUNCA** aceptar la API key como valor literal de flag (solo prompt/stdin/env). Redactar la key en toda salida.
- stdout = datos; stderr = mensajes/banner/errores. `--json` emite el body crudo de la API. `--json` + `--plain` → exit 2.
- Mensajes de usuario en **español** (regla de producto).
- TDD estricto: test que falla → mínimo para pasar → commit. Commits frecuentes, uno por task como mínimo.
- **Deviación del spec, justificada:** se usa un paquete de config tipado con `go-toml/v2` en vez de Viper (Viper mantiene estado global que dificulta los tests; Cobra ya cubre flags). Anotado para confirmación.

---

### Task 1: Scaffolding del módulo y comando raíz

**Files:**
- Create: `go.mod`
- Create: `cmd/factuarea/main.go`
- Create: `internal/cmd/root.go`
- Create: `internal/cmd/version.go`
- Create: `internal/buildinfo/buildinfo.go`
- Create: `Makefile`
- Test: `internal/cmd/version_test.go`

**Interfaces:**
- Produces: `cmd.NewRootCmd() *cobra.Command` (raíz con flags persistentes globales); `buildinfo.Version`, `buildinfo.Commit`, `buildinfo.SpecHash` (vars inyectables por ldflags).

- [ ] **Step 1: Inicializar el módulo y dependencias**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli
go mod init github.com/factuarea/factuarea-cli
go get github.com/spf13/cobra@latest github.com/pelletier/go-toml/v2@latest github.com/zalando/go-keyring@latest
```
Expected: `go.mod` creado con las 3 dependencias.

- [ ] **Step 2: Escribir el test del comando version**

Create `internal/cmd/version_test.go`:
```go
package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "factuarea") {
		t.Fatalf("version output missing binary name: %q", out.String())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/cmd/ -run TestVersionCommand -v`
Expected: FAIL (no compila: `NewRootCmd` indefinido).

- [ ] **Step 4: Implementar buildinfo, root y version**

Create `internal/buildinfo/buildinfo.go`:
```go
package buildinfo

// Inyectables por -ldflags en release. Defaults para `go run`/dev.
var (
	Version  = "dev"
	Commit   = "none"
	SpecHash = "unknown" // hash del openapi.json embebido (lo fija el Plan 2)
)
```

Create `internal/cmd/root.go`:
```go
package cmd

import "github.com/spf13/cobra"

// GlobalFlags contiene los flags persistentes compartidos por todos los comandos.
type GlobalFlags struct {
	JSON    bool
	Plain   bool
	NoColor bool
	NoInput bool
	Profile string
	Live    bool
	Verbose bool
	Quiet   bool
}

func NewRootCmd() *cobra.Command {
	g := &GlobalFlags{}
	root := &cobra.Command{
		Use:           "factuarea",
		Short:         "CLI oficial de Factuarea — maneja la API pública v1 desde la terminal",
		SilenceUsage:  true, // los errores se imprimen una vez, sin volcar el usage entero
		SilenceErrors: true, // el control de exit code lo lleva main.go
	}
	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "salida JSON cruda (para scripts/agentes)")
	pf.BoolVar(&g.Plain, "plain", false, "salida en texto plano sin formato")
	pf.BoolVar(&g.NoColor, "no-color", false, "desactiva el color")
	pf.BoolVar(&g.NoInput, "no-input", false, "no preguntar nada de forma interactiva")
	pf.StringVar(&g.Profile, "profile", "", "perfil de configuración a usar")
	pf.BoolVar(&g.Live, "live", false, "permite operaciones mutadoras en entorno LIVE")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "salida detallada")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "silencia mensajes informativos")

	root.AddCommand(newVersionCmd())
	return root
}
```

Create `internal/cmd/version.go`:
```go
package cmd

import (
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión del CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "factuarea %s (commit %s, spec %s)\n",
				buildinfo.Version, buildinfo.Commit, buildinfo.SpecHash)
			return nil
		},
	}
}
```

Create `cmd/factuarea/main.go`:
```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

func main() {
	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		// El mensaje ya se ha redactado/impreso por el comando cuando aplica;
		// aquí garantizamos al menos una línea en stderr y el exit code correcto.
		var silent *cmd.AlreadyReported
		if !errors.As(err, &silent) {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(exit.ForError(err))
	}
}
```

Add to `internal/cmd/root.go` (tipo centinela para errores ya impresos por el comando):
```go
// AlreadyReported envuelve un error cuyo mensaje ya fue impreso por el comando,
// para que main.go no lo duplique pero sí derive el exit code.
type AlreadyReported struct{ Err error }

func (e *AlreadyReported) Error() string { return e.Err.Error() }
func (e *AlreadyReported) Unwrap() error { return e.Err }
```

> Nota: `internal/exit` se crea en la Task 2; este código no compila hasta entonces. Implementar Task 2 inmediatamente después.

- [ ] **Step 5: Crear el Makefile**

Create `Makefile`:
```makefile
BINARY := factuarea
PKG := github.com/factuarea/factuarea-cli

.PHONY: build test lint fmt run
build:
	go build -o $(BINARY) ./cmd/factuarea
test:
	go test ./...
fmt:
	gofmt -s -w .
lint:
	go vet ./...
run:
	go run ./cmd/factuarea $(ARGS)
```

- [ ] **Step 6: Run test (tras completar Task 2) y commit**

Run: `go test ./internal/cmd/ -run TestVersionCommand -v`
Expected: PASS.

```bash
git add go.mod go.sum cmd internal Makefile
git commit -m "feat: scaffold Go module, root command and version"
```

---

### Task 2: Modelo de error y exit codes

**Files:**
- Create: `internal/apierr/apierr.go`
- Create: `internal/exit/exit.go`
- Test: `internal/exit/exit_test.go`

**Interfaces:**
- Produces: `apierr.APIError{StatusCode,Type,Code,Message,Subcode,Param,DocURL,RequestID}` (implementa `error`); `apierr.TransportError{Err}` (implementa `error`+`Unwrap`); `exit.ForError(error) int` y las constantes `exit.OK..exit.Network`.

- [ ] **Step 1: Escribir el test del mapeo de exit codes**

Create `internal/exit/exit_test.go`:
```go
package exit

import (
	"errors"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestForError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"auth", &apierr.APIError{Type: "authentication_error"}, Auth},
		{"perm", &apierr.APIError{Type: "authorization_error"}, Perm},
		{"validation", &apierr.APIError{Type: "invalid_request_error"}, Validation},
		{"notfound", &apierr.APIError{Type: "not_found_error"}, NotFound},
		{"ratelimit", &apierr.APIError{Type: "rate_limit_error"}, RateLimit},
		{"conflict", &apierr.APIError{Type: "conflict_error"}, Conflict},
		{"idempotency", &apierr.APIError{Type: "idempotency_error"}, Conflict},
		{"server", &apierr.APIError{Type: "api_error"}, Server},
		{"unavailable", &apierr.APIError{Type: "service_unavailable_error"}, Server},
		{"transport", &apierr.TransportError{Err: errors.New("dial tcp: timeout")}, Network},
		{"unknown api type", &apierr.APIError{Type: "weird"}, Server},
		{"generic", errors.New("boom"), CLIBug},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ForError(c.err); got != c.want {
				t.Fatalf("ForError(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/exit/ -v`
Expected: FAIL (paquetes indefinidos).

- [ ] **Step 3: Implementar apierr y exit**

Create `internal/apierr/apierr.go`:
```go
package apierr

// APIError representa el sobre de error del backend de Factuarea.
type APIError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
	Subcode    string
	Param      string
	DocURL     string
	RequestID  string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return e.Message + " (" + e.Code + ")"
	}
	return e.Message
}

// TransportError representa un fallo de red/timeout/TLS: no hubo respuesta HTTP
// con sobre de error. Es transitorio y reintentable.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }
```

Create `internal/exit/exit.go`:
```go
package exit

import (
	"errors"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

const (
	OK         = 0
	CLIBug     = 1  // bug inesperado del propio CLI (no errores de API)
	Usage      = 2  // flags/args inválidos o guard local (--live/--confirm/trigger en live)
	Auth       = 3
	Perm       = 4  // 403 o scope insuficiente local
	Validation = 5
	NotFound   = 6
	RateLimit  = 7
	Conflict   = 8
	Server     = 9  // api_error / service_unavailable_error / 503 kill-switch / 5xx sin body
	Network    = 10 // red/timeout/DNS/TLS, transitorio
)

// ForError deriva el exit code. Si hay sobre de error del backend, se deriva de
// error.type; un fallo de transporte → Network; cualquier otro → CLIBug.
func ForError(err error) int {
	if err == nil {
		return OK
	}
	var api *apierr.APIError
	if errors.As(err, &api) {
		switch api.Type {
		case "authentication_error":
			return Auth
		case "authorization_error":
			return Perm
		case "invalid_request_error":
			return Validation
		case "not_found_error":
			return NotFound
		case "rate_limit_error":
			return RateLimit
		case "conflict_error", "idempotency_error":
			return Conflict
		case "api_error", "service_unavailable_error":
			return Server
		default:
			return Server
		}
	}
	var transport *apierr.TransportError
	if errors.As(err, &transport) {
		return Network
	}
	return CLIBug
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/exit/ -v`
Expected: PASS (todos los casos).

- [ ] **Step 5: Commit**

```bash
git add internal/apierr internal/exit
git commit -m "feat: API error model and semantic exit codes"
```

---

### Task 3: Runtime HTTP client (Do + parseo de error + retries + idempotencia)

**Files:**
- Create: `internal/client/client.go`
- Create: `internal/client/errors.go`
- Test: `internal/client/client_test.go`

**Interfaces:**
- Consumes: `apierr.APIError`, `apierr.TransportError`.
- Produces:
  - `client.New(apiKey string, opts ...client.Option) *client.Client`
  - `client.WithBaseURL(string) Option`, `client.WithHTTPClient(*http.Client) Option`, `client.WithMaxRetries(int) Option`, `client.WithClock(func() time.Time) Option`
  - `(*Client).Do(ctx context.Context, method, path string, body []byte, extraHeaders map[string]string) (*client.Response, error)`
  - `client.Response{StatusCode int; Header http.Header; Body []byte; ContentType string; RequestID string}`

- [ ] **Step 1: Escribir los tests del cliente (httptest)**

Create `internal/client/client_test.go`:
```go
package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa",
		WithBaseURL(srv.URL),
		WithMaxRetries(2),
		WithClock(func() time.Time { return time.Unix(0, 0) }),
	)
	return c, srv
}

func TestDoSendsBearerAndReturnsBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Errorf("bad auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_123")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})
	resp, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 || string(resp.Body) != `{"id":"x"}` {
		t.Fatalf("unexpected resp: %d %s", resp.StatusCode, resp.Body)
	}
	if resp.RequestID != "req_123" {
		t.Fatalf("missing request id: %q", resp.RequestID)
	}
}

func TestDoParsesErrorEnvelope(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invoice_invalid_total","message":"Total inválido","request_id":"req_9"}}`))
	})
	_, err := c.Do(context.Background(), "POST", "/v1/invoices", []byte(`{}`), nil)
	var api *apierr.APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected APIError, got %T %v", err, err)
	}
	if api.Type != "invalid_request_error" || api.Code != "invoice_invalid_total" || api.StatusCode != 422 {
		t.Fatalf("bad parsed error: %+v", api)
	}
}

func TestDoNonJSONErrorBecomesGenericAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`<html>service unavailable</html>`))
	})
	_, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	var api *apierr.APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if api.Type != "service_unavailable_error" || api.StatusCode != 503 {
		t.Fatalf("bad synthesized error: %+v", api)
	}
}

func TestDoRetriesOn500ThenSucceeds(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	resp, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls and 200, got %d calls / %d", calls, resp.StatusCode)
	}
}

func TestDoAddsIdempotencyKeyOnPOST(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.Header.Get("Idempotency-Key") == "" {
			t.Error("POST without Idempotency-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := c.Do(context.Background(), "POST", "/v1/invoices", []byte(`{}`), nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/client/ -v`
Expected: FAIL (paquete indefinido).

- [ ] **Step 3: Implementar el cliente**

Create `internal/client/client.go`:
```go
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"math"
	"net/http"
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
	now        func() time.Time
	sleep      func(time.Duration)
}

type Option func(*Client)

func WithBaseURL(u string) Option        { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }
func WithMaxRetries(n int) Option        { return func(c *Client) { c.maxRetries = n } }
func WithAPIVersion(v string) Option     { return func(c *Client) { c.apiVersion = v } }
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		hc:         &http.Client{Timeout: 60 * time.Second},
		maxRetries: 3,
		now:        time.Now,
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
	// 200ms, 400ms, 800ms... (jitter omitido en tests por el WithClock; se puede
	// añadir jitter real en producción sin afectar a la lógica de reintento).
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
```

Create `internal/client/errors.go`:
```go
package client

import (
	"encoding/json"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// parseError construye un APIError. Si el cuerpo es JSON con sobre {"error":{...}}
// se usa tal cual; si no (kill-switch 503, 5xx sin body, HTML de proxy), se
// sintetiza el type a partir del status code para que el exit code sea correcto.
func parseError(resp *Response) error {
	if strings.Contains(resp.ContentType, "json") {
		var env struct {
			Error struct {
				Type, Code, Message, Subcode, Param, DocURL, RequestID string
			} `json:"error"`
		}
		// doc_url / request_id vienen en snake_case en el JSON:
		var raw struct {
			Error map[string]any `json:"error"`
		}
		if json.Unmarshal(resp.Body, &raw) == nil && raw.Error != nil {
			get := func(k string) string {
				if v, ok := raw.Error[k].(string); ok {
					return v
				}
				return ""
			}
			api := &apierr.APIError{
				StatusCode: resp.StatusCode,
				Type:       get("type"),
				Code:       get("code"),
				Message:    get("message"),
				Subcode:    get("subcode"),
				Param:      get("param"),
				DocURL:     get("doc_url"),
				RequestID:  get("request_id"),
			}
			if api.RequestID == "" {
				api.RequestID = resp.RequestID
			}
			if api.Type == "" {
				api.Type = synthesizeType(resp.StatusCode)
			}
			if api.Message == "" {
				api.Message = "Error " + http_status(resp.StatusCode)
			}
			return api
		}
		_ = env
	}
	return &apierr.APIError{
		StatusCode: resp.StatusCode,
		Type:       synthesizeType(resp.StatusCode),
		Message:    "Error " + http_status(resp.StatusCode),
		RequestID:  resp.RequestID,
	}
}

func synthesizeType(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "authorization_error"
	case status == 404:
		return "not_found_error"
	case status == 409:
		return "conflict_error"
	case status == 429:
		return "rate_limit_error"
	case status == 503:
		return "service_unavailable_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func http_status(code int) string {
	switch code {
	case 503:
		return "503 (servicio no disponible)"
	default:
		return "HTTP " + itoa(code)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
```

> Nota de implementación: el bloque `env` quedó sin uso tras simplificar a `map[string]any` para leer las claves snake_case; el implementador debe **eliminar la struct `env`** y su `_ = env` al integrar (limpieza propia del cambio). Mantener solo el camino `raw`.

- [ ] **Step 4: Limpiar el código muerto y verificar compilación**

Eliminar la struct `env` y la línea `_ = env` de `errors.go` (es residuo de la primera redacción). Sustituir `itoa`/`http_status` por `strconv.Itoa` si se prefiere (importando `strconv`).

Run: `go vet ./internal/client/`
Expected: sin errores ni variables sin uso.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/client/ -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/client
git commit -m "feat: HTTP runtime client with retries, idempotency and error parsing"
```

---

### Task 4: Entorno desde el prefijo de key y resolución de credenciales

**Files:**
- Create: `internal/config/env.go`
- Create: `internal/config/resolve.go`
- Test: `internal/config/env_test.go`
- Test: `internal/config/resolve_test.go`

**Interfaces:**
- Produces:
  - `config.Environment(apiKey string) string` → `"test"`, `"live"` o `"unknown"`.
  - `config.ValidKeyFormat(apiKey string) bool`.
  - `config.RedactKey(apiKey string) string` (p.ej. `fact_test_…aaaa`).
  - `config.Store` interface (implementada en Task 5): `GetKey(profile string) (string, error)`, `SetKey(profile, key string) error`, `DeleteKey(profile string) error`.
  - `config.Resolution{APIKey, Source, Profile, Environment string}`.
  - `config.ResolveAPIKey(stdinKey string, profile string, getenv func(string) string, store Store) (Resolution, error)`.

- [ ] **Step 1: Tests de entorno y resolución**

Create `internal/config/env_test.go`:
```go
package config

import "testing"

func TestEnvironment(t *testing.T) {
	cases := map[string]string{
		"fact_test_aaaaaaaaaaaaaaaaaaaaaaaa": "test",
		"fact_live_bbbbbbbbbbbbbbbbbbbbbbbb": "live",
		"sk_other":                          "unknown",
		"":                                  "unknown",
	}
	for k, want := range cases {
		if got := Environment(k); got != want {
			t.Errorf("Environment(%q)=%q want %q", k, got, want)
		}
	}
}

func TestRedactKey(t *testing.T) {
	got := RedactKey("fact_test_abcdefghijklmnopqrstuvwx")
	if got == "fact_test_abcdefghijklmnopqrstuvwx" || got == "" {
		t.Fatalf("key not redacted: %q", got)
	}
	if want := "fact_test_…uvwx"; got != want {
		t.Fatalf("RedactKey = %q want %q", got, want)
	}
}
```

Create `internal/config/resolve_test.go`:
```go
package config

import (
	"errors"
	"testing"
)

type fakeStore struct{ keys map[string]string }

func (f *fakeStore) GetKey(p string) (string, error) {
	if k, ok := f.keys[p]; ok {
		return k, nil
	}
	return "", errors.New("not found")
}
func (f *fakeStore) SetKey(p, k string) error { f.keys[p] = k; return nil }
func (f *fakeStore) DeleteKey(p string) error { delete(f.keys, p); return nil }

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolvePrecedenceStdinWins(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("fact_live_ssssssssssssssssssssssss", "", env(map[string]string{"FACTUAREA_API_KEY": "fact_test_eeeeeeeeeeeeeeeeeeeeeeee"}), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "stdin" || r.Environment != "live" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveEnvOverProfile(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("", "", env(map[string]string{"FACTUAREA_API_KEY": "fact_test_eeeeeeeeeeeeeeeeeeeeeeee"}), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "env" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveProfileFallback(t *testing.T) {
	st := &fakeStore{keys: map[string]string{"default": "fact_test_dddddddddddddddddddddddd"}}
	r, err := ResolveAPIKey("", "", env(nil), st)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "profile" || r.Profile != "default" || r.Environment != "test" {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveNoCredentials(t *testing.T) {
	st := &fakeStore{keys: map[string]string{}}
	if _, err := ResolveAPIKey("", "", env(nil), st); err == nil {
		t.Fatal("expected error when no credentials")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v`
Expected: FAIL.

- [ ] **Step 3: Implementar env.go y resolve.go**

Create `internal/config/env.go`:
```go
package config

import "regexp"

var keyRe = regexp.MustCompile(`^(fact_live_|fact_test_)[A-Za-z0-9]{24}$`)

func Environment(apiKey string) string {
	switch {
	case len(apiKey) >= 10 && apiKey[:10] == "fact_test_":
		return "test"
	case len(apiKey) >= 10 && apiKey[:10] == "fact_live_":
		return "live"
	default:
		return "unknown"
	}
}

func ValidKeyFormat(apiKey string) bool { return keyRe.MatchString(apiKey) }

// RedactKey deja visible el prefijo de entorno y los últimos 4 caracteres.
func RedactKey(apiKey string) string {
	if len(apiKey) < 14 {
		return "…"
	}
	return apiKey[:10] + "…" + apiKey[len(apiKey)-4:]
}
```

Create `internal/config/resolve.go`:
```go
package config

import "errors"

// Store persiste credenciales por profile (keyring con fallback a archivo).
type Store interface {
	GetKey(profile string) (string, error)
	SetKey(profile, key string) error
	DeleteKey(profile string) error
}

type Resolution struct {
	APIKey      string
	Source      string // "stdin" | "env" | "profile"
	Profile     string
	Environment string // "test" | "live" | "unknown"
}

const (
	EnvAPIKey  = "FACTUAREA_API_KEY"
	EnvProfile = "FACTUAREA_PROFILE"
)

// ResolveAPIKey aplica la precedencia: stdin/flag > env > profile.
// `profile` vacío usa "default" (o FACTUAREA_PROFILE si está). La fuente de
// verdad del entorno es SIEMPRE el prefijo de la key resuelta.
func ResolveAPIKey(stdinKey, profile string, getenv func(string) string, store Store) (Resolution, error) {
	if profile == "" {
		profile = getenv(EnvProfile)
	}
	if profile == "" {
		profile = "default"
	}

	if stdinKey != "" {
		return mk(stdinKey, "stdin", profile), nil
	}
	if v := getenv(EnvAPIKey); v != "" {
		return mk(v, "env", profile), nil
	}
	if store != nil {
		if k, err := store.GetKey(profile); err == nil && k != "" {
			return mk(k, "profile", profile), nil
		}
	}
	return Resolution{}, errors.New("no hay credenciales: ejecuta `factuarea login` o define FACTUAREA_API_KEY")
}

func mk(key, source, profile string) Resolution {
	return Resolution{APIKey: key, Source: source, Profile: profile, Environment: Environment(key)}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/env.go internal/config/resolve.go internal/config/env_test.go internal/config/resolve_test.go
git commit -m "feat: environment-from-prefix and credential resolution precedence"
```

---

### Task 5: Store de credenciales (keyring + fallback a TOML)

**Files:**
- Create: `internal/config/store.go`
- Create: `internal/config/paths.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `config.Store` (definido en Task 4).
- Produces: `config.NewStore() (Store, bool)` → devuelve el store activo y un bool `usingFallback` (true si el keyring no está disponible y se usó archivo, para que el comando avise). `config.NewFileStore(path string) Store` (usado en tests).

- [ ] **Step 1: Test del file store (el keyring real no se testea en unit; se hace smoke en Plan 4)**

Create `internal/config/store_test.go`:
```go
package config

import (
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	st := NewFileStore(path)

	if _, err := st.GetKey("default"); err == nil {
		t.Fatal("expected error for missing key")
	}
	if err := st.SetKey("default", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetKey("default")
	if err != nil || got != "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("roundtrip failed: %q %v", got, err)
	}
	// Segundo profile no pisa al primero.
	if err := st.SetKey("acme-live", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetKey("default"); got != "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("second profile clobbered first: %q", got)
	}
	if err := st.DeleteKey("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetKey("default"); err == nil {
		t.Fatal("expected error after delete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestFileStore -v`
Expected: FAIL (`NewFileStore` indefinido).

- [ ] **Step 3: Implementar paths.go y store.go**

Create `internal/config/paths.go`:
```go
package config

import (
	"os"
	"path/filepath"
)

// ConfigDir devuelve ~/.config/factuarea (respeta XDG_CONFIG_HOME).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "factuarea"), nil
}

func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}
```

Create `internal/config/store.go`:
```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/zalando/go-keyring"
)

const keyringService = "factuarea-cli"

var errNotFound = errors.New("credencial no encontrada")

// keyringStore guarda cada key en el keyring del SO bajo el servicio
// "factuarea-cli" y la cuenta = nombre del profile.
type keyringStore struct{}

func (keyringStore) GetKey(profile string) (string, error) {
	k, err := keyring.Get(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errNotFound
	}
	return k, err
}
func (keyringStore) SetKey(profile, key string) error { return keyring.Set(keyringService, profile, key) }
func (keyringStore) DeleteKey(profile string) error {
	err := keyring.Delete(keyringService, profile)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// fileStore es el fallback: TOML en ~/.config/factuarea/config.toml, chmod 600.
type fileStore struct {
	path string
	mu   sync.Mutex
}

type fileDoc struct {
	Profiles map[string]profileEntry `toml:"profiles"`
}
type profileEntry struct {
	APIKey string `toml:"api_key"`
}

func NewFileStore(path string) Store { return &fileStore{path: path} }

func (s *fileStore) load() (fileDoc, error) {
	var doc fileDoc
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		doc.Profiles = map[string]profileEntry{}
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := toml.Unmarshal(b, &doc); err != nil {
		return doc, err
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string]profileEntry{}
	}
	return doc, nil
}

func (s *fileStore) save(doc fileDoc) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	// Escritura atómica: archivo temporal + rename, con permisos 600.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *fileStore) GetKey(profile string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return "", err
	}
	e, ok := doc.Profiles[profile]
	if !ok || e.APIKey == "" {
		return "", errNotFound
	}
	return e.APIKey, nil
}

func (s *fileStore) SetKey(profile, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	doc.Profiles[profile] = profileEntry{APIKey: key}
	return s.save(doc)
}

func (s *fileStore) DeleteKey(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	delete(doc.Profiles, profile)
	return s.save(doc)
}

// NewStore prueba el keyring del SO; si no está disponible, cae al archivo y
// devuelve usingFallback=true para que el comando avise (nunca degrada en silencio).
func NewStore() (Store, bool) {
	// Probe del keyring: set+get+delete de un valor centinela.
	const probe = "__factuarea_probe__"
	if err := keyring.Set(keyringService, probe, "1"); err == nil {
		_ = keyring.Delete(keyringService, probe)
		return keyringStore{}, false
	}
	path, err := ConfigFile()
	if err != nil {
		path = filepath.Join(os.TempDir(), "factuarea-config.toml")
	}
	return NewFileStore(path), true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (incluido el roundtrip de file store).

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/paths.go internal/config/store_test.go
git commit -m "feat: credential store (OS keyring with transparent TOML fallback)"
```

---

### Task 6: Capa de output (formato + render) y detección TTY

**Files:**
- Create: `internal/output/format.go`
- Create: `internal/output/render.go`
- Test: `internal/output/format_test.go`

**Interfaces:**
- Consumes: `apierr.APIError`.
- Produces:
  - `output.Format` (`Human`, `JSON`, `Plain`) + `output.ResolveFormat(jsonFlag, plainFlag, isTTY bool) (Format, error)`.
  - `output.PrintBody(w io.Writer, body []byte, f Format) error` (en JSON/Plain vuelca el body crudo; en Human idem por ahora — el render de tablas llega en Plan 2).
  - `output.PrintError(stderr io.Writer, err error, f Format)` (humano legible o JSON `{"error":{...}}`).
  - `output.IsTTY(f *os.File) bool`.

- [ ] **Step 1: Tests de ResolveFormat y PrintError**

Create `internal/output/format_test.go`:
```go
package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestResolveFormat(t *testing.T) {
	if _, err := ResolveFormat(true, true, false); err == nil {
		t.Fatal("json+plain must error")
	}
	if f, _ := ResolveFormat(true, false, true); f != JSON {
		t.Fatal("explicit --json must win over TTY")
	}
	if f, _ := ResolveFormat(false, false, true); f != Human {
		t.Fatal("TTY default is Human")
	}
	if f, _ := ResolveFormat(false, false, false); f != JSON {
		t.Fatal("non-TTY default is JSON")
	}
}

func TestPrintErrorJSON(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, &apierr.APIError{Type: "not_found_error", Code: "invoice_not_found", Message: "No existe", RequestID: "req_1"}, JSON)
	s := buf.String()
	if !strings.Contains(s, `"type":"not_found_error"`) || !strings.Contains(s, `"request_id":"req_1"`) {
		t.Fatalf("bad json error: %s", s)
	}
}

func TestPrintErrorHuman(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, &apierr.APIError{Code: "invoice_not_found", Message: "No existe la factura", DocURL: "https://docs.factuarea.com/x"}, Human)
	s := buf.String()
	if !strings.Contains(s, "No existe la factura") || !strings.Contains(s, "docs.factuarea.com") {
		t.Fatalf("bad human error: %s", s)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/output/ -v`
Expected: FAIL.

- [ ] **Step 3: Implementar format.go y render.go**

Create `internal/output/format.go`:
```go
package output

import (
	"errors"
	"os"
)

type Format int

const (
	Human Format = iota
	JSON
	Plain
)

// ResolveFormat: flag explícito > autodetección TTY. --json y --plain son
// mutuamente excluyentes. Sin flags: Human en TTY, JSON fuera de TTY.
func ResolveFormat(jsonFlag, plainFlag, isTTY bool) (Format, error) {
	if jsonFlag && plainFlag {
		return Human, errors.New("--json y --plain son mutuamente excluyentes")
	}
	if jsonFlag {
		return JSON, nil
	}
	if plainFlag {
		return Plain, nil
	}
	if isTTY {
		return Human, nil
	}
	return JSON, nil
}

func IsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
```

Create `internal/output/render.go`:
```go
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// PrintBody vuelca el cuerpo de respuesta. En JSON/Plain se emite crudo (es el
// body exacto de la API). En Human, por ahora también crudo; el render de
// tablas se añade en el Plan 2 sobre el spec.
func PrintBody(w io.Writer, body []byte, f Format) error {
	_, err := w.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
	return err
}

// PrintError escribe el error a stderr: JSON estructurado bajo --json, legible
// en humano. Mantiene el shape del backend.
func PrintError(stderr io.Writer, err error, f Format) {
	var api *apierr.APIError
	if errors.As(err, &api) {
		if f == JSON {
			payload := map[string]any{"error": map[string]any{
				"type": api.Type, "code": api.Code, "message": api.Message,
				"subcode": api.Subcode, "param": api.Param,
				"doc_url": api.DocURL, "request_id": api.RequestID,
			}}
			enc := json.NewEncoder(stderr)
			_ = enc.Encode(payload)
			return
		}
		fmt.Fprintf(stderr, "Error: %s\n", api.Message)
		if api.Code != "" {
			fmt.Fprintf(stderr, "  código: %s\n", api.Code)
		}
		if api.RequestID != "" {
			fmt.Fprintf(stderr, "  request_id: %s\n", api.RequestID)
		}
		if api.DocURL != "" {
			fmt.Fprintf(stderr, "  más info: %s\n", api.DocURL)
		}
		return
	}
	if f == JSON {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	fmt.Fprintf(stderr, "Error: %s\n", err.Error())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/output/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/output
git commit -m "feat: output format resolution and error rendering"
```

---

### Task 7: Guards fiscales (`--live` y confirmación tipada)

**Files:**
- Create: `internal/safety/guard.go`
- Test: `internal/safety/guard_test.go`

**Interfaces:**
- Produces:
  - `safety.RequireLive(environment string, liveFlag bool) error` — falla (Usage) si `environment=="live"` y `!liveFlag`.
  - `safety.RequireSandbox(environment string) error` — falla si `environment!="test"` (para `trigger`).
  - `safety.Confirm(resourceID, confirmFlag string, isTTY, noInput bool, prompt func(string) (string, error)) error` — exige `--confirm=<id>` exacto; nunca bloquea esperando stdin en no-TTY/`--no-input`.

- [ ] **Step 1: Tests de los guards**

Create `internal/safety/guard_test.go`:
```go
package safety

import "testing"

func TestRequireLive(t *testing.T) {
	if err := RequireLive("live", false); err == nil {
		t.Fatal("live without --live must fail")
	}
	if err := RequireLive("live", true); err != nil {
		t.Fatal("live with --live must pass")
	}
	if err := RequireLive("test", false); err != nil {
		t.Fatal("test never needs --live")
	}
}

func TestRequireSandbox(t *testing.T) {
	if err := RequireSandbox("live"); err == nil {
		t.Fatal("trigger must reject live")
	}
	if err := RequireSandbox("test"); err != nil {
		t.Fatal("trigger must allow test")
	}
}

func TestConfirmNeverBlocksInNoInput(t *testing.T) {
	called := false
	prompt := func(string) (string, error) { called = true; return "", nil }
	// Sin --confirm y no-input: falla inmediatamente, sin promptear.
	if err := Confirm("inv_1", "", false, true, prompt); err == nil {
		t.Fatal("expected immediate failure")
	}
	if called {
		t.Fatal("must not prompt in no-input")
	}
}

func TestConfirmFlagMatch(t *testing.T) {
	if err := Confirm("inv_1", "inv_1", false, true, nil); err != nil {
		t.Fatal("matching --confirm must pass")
	}
	if err := Confirm("inv_1", "inv_2", false, true, nil); err == nil {
		t.Fatal("mismatched --confirm must fail")
	}
}

func TestConfirmInteractivePrompt(t *testing.T) {
	prompt := func(string) (string, error) { return "inv_1", nil }
	if err := Confirm("inv_1", "", true, false, prompt); err != nil {
		t.Fatalf("interactive matching prompt must pass: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/safety/ -v`
Expected: FAIL.

- [ ] **Step 3: Implementar guard.go**

Create `internal/safety/guard.go`:
```go
package safety

import (
	"fmt"
	"strings"
)

// RequireLive exige el flag --live para operaciones mutadoras en entorno live.
func RequireLive(environment string, liveFlag bool) error {
	if environment == "live" && !liveFlag {
		return fmt.Errorf("operación en entorno LIVE: añade --live para confirmar que NO es una prueba")
	}
	return nil
}

// RequireSandbox exige entorno sandbox (key fact_test_). Lo usa `trigger`.
func RequireSandbox(environment string) error {
	if environment != "test" {
		return fmt.Errorf("este comando solo funciona en sandbox: usa una key fact_test_ (entorno actual: %s)", environment)
	}
	return nil
}

// Confirm exige confirmación tipada del id exacto para operaciones irreversibles.
// Nunca bloquea esperando stdin: si no es TTY o es --no-input, falla de inmediato.
func Confirm(resourceID, confirmFlag string, isTTY, noInput bool, prompt func(string) (string, error)) error {
	if confirmFlag != "" {
		if confirmFlag == resourceID {
			return nil
		}
		return fmt.Errorf("--confirm=%q no coincide con %q", confirmFlag, resourceID)
	}
	if noInput || !isTTY {
		return fmt.Errorf("acción irreversible: pasa --confirm=%s para confirmarla", resourceID)
	}
	typed, err := prompt(fmt.Sprintf("Esto es IRREVERSIBLE. Escribe %q para confirmar: ", resourceID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(typed) != resourceID {
		return fmt.Errorf("confirmación cancelada (no coincidió con %q)", resourceID)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/safety/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/safety
git commit -m "feat: fiscal safety guards (--live, sandbox-only, typed confirmation)"
```

---

### Task 8: Comandos `login`, `logout`, `whoami` y `api` (end-to-end)

**Files:**
- Create: `internal/cmd/context.go`
- Create: `internal/cmd/login.go`
- Create: `internal/cmd/logout.go`
- Create: `internal/cmd/whoami.go`
- Create: `internal/cmd/api.go`
- Modify: `internal/cmd/root.go` (registrar los comandos)
- Test: `internal/cmd/api_test.go`

**Interfaces:**
- Consumes: `client`, `config`, `output`, `safety`, `apierr`, `exit`.
- Produces: `cmd.cliContext` (helper que resuelve credenciales + cliente + formato a partir de los flags globales) y los 4 comandos.

- [ ] **Step 1: Test del comando `api` contra un backend simulado**

Create `internal/cmd/api_test.go`:
```go
package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPICommandGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"acct_1","object":"account"}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"api", "get", "/v1/account", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), `"id":"acct_1"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestAPICommandLiveGuardBlocksMutation(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// POST en live sin --live debe fallar ANTES de cualquier red.
	root.SetArgs([]string{"api", "post", "/v1/invoices", "-d", "{}"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "LIVE") {
		t.Fatalf("expected live guard error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestAPICommand -v`
Expected: FAIL.

- [ ] **Step 3: Implementar el helper de contexto**

Create `internal/cmd/context.go`:
```go
package cmd

import (
	"os"

	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

// cliContext resuelve credenciales, cliente y formato a partir de los flags.
type cliContext struct {
	res    config.Resolution
	client *client.Client
	format output.Format
	g      *GlobalFlags
}

const envBaseURL = "FACTUAREA_BASE_URL" // override para tests/staging

func newCLIContext(g *GlobalFlags, stdinKey string) (*cliContext, error) {
	store, _ := config.NewStore()
	res, err := config.ResolveAPIKey(stdinKey, g.Profile, os.Getenv, store)
	if err != nil {
		return nil, err
	}
	opts := []client.Option{}
	if base := os.Getenv(envBaseURL); base != "" {
		opts = append(opts, client.WithBaseURL(base))
	}
	f, err := output.ResolveFormat(g.JSON, g.Plain, output.IsTTY(os.Stdout))
	if err != nil {
		return nil, err
	}
	return &cliContext{
		res:    res,
		client: client.New(res.APIKey, opts...),
		format: f,
		g:      g,
	}, nil
}

// globalsFrom recupera el puntero a GlobalFlags guardado por NewRootCmd.
func globalsFrom(cmd *cobra.Command) *GlobalFlags {
	return cmd.Context().Value(globalsKey{}).(*GlobalFlags)
}

type globalsKey struct{}
```

Modify `internal/cmd/root.go` — inyectar `GlobalFlags` en el context y registrar comandos. Sustituir el cuerpo de `NewRootCmd` por:
```go
func NewRootCmd() *cobra.Command {
	g := &GlobalFlags{}
	root := &cobra.Command{
		Use:           "factuarea",
		Short:         "CLI oficial de Factuarea — maneja la API pública v1 desde la terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "salida JSON cruda (para scripts/agentes)")
	pf.BoolVar(&g.Plain, "plain", false, "salida en texto plano sin formato")
	pf.BoolVar(&g.NoColor, "no-color", false, "desactiva el color")
	pf.BoolVar(&g.NoInput, "no-input", false, "no preguntar nada de forma interactiva")
	pf.StringVar(&g.Profile, "profile", "", "perfil de configuración a usar")
	pf.BoolVar(&g.Live, "live", false, "permite operaciones mutadoras en entorno LIVE")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "salida detallada")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "silencia mensajes informativos")

	root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		ctx := context.WithValue(cmd.Context(), globalsKey{}, g)
		cmd.SetContext(ctx)
	}

	root.AddCommand(
		newVersionCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newWhoamiCmd(),
		newAPICmd(),
	)
	return root
}
```
Añadir `import "context"` al `root.go`.

- [ ] **Step 4: Implementar login/logout/whoami**

Create `internal/cmd/login.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"os"
)

func newLoginCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Guarda tu API key (fact_test_… o fact_live_…)",
		Long:  "Lee la API key por prompt oculto, por stdin (--api-key -) o por la env FACTUAREA_API_KEY.\nNUNCA la pases como valor literal de flag.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			key, err := readKey(cmd, fromStdin, g.NoInput)
			if err != nil {
				return err
			}
			key = strings.TrimSpace(key)
			if !config.ValidKeyFormat(key) {
				return fmt.Errorf("formato de key inválido (se espera fact_test_… o fact_live_… con 24 caracteres)")
			}
			profile := g.Profile
			if profile == "" {
				profile = "default"
			}
			// Validar contra el backend antes de guardar.
			opts := []client.Option{}
			if base := os.Getenv(envBaseURL); base != "" {
				opts = append(opts, client.WithBaseURL(base))
			}
			c := client.New(key, opts...)
			if _, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil); err != nil {
				return fmt.Errorf("la key no validó contra la API: %w", err)
			}
			store, fallback := config.NewStore()
			if err := store.SetKey(profile, key); err != nil {
				return err
			}
			env := config.Environment(key)
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Sesión guardada (perfil %q, entorno %s).\n", profile, strings.ToUpper(env))
			if env == "live" {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ ATENCIÓN: esta es una key LIVE. Las operaciones mutadoras afectarán datos reales y AEAT.")
			}
			if fallback {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ El keyring del sistema no está disponible; la key se guardó en ~/.config/factuarea/config.toml (permisos 600).")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "api-key", false, "lee la key por stdin cuando se pasa `-` (no acepta el valor literal)")
	cmd.Flags().Lookup("api-key").NoOptDefVal = "false"
	return cmd
}

// readKey: stdin si fromStdin/no-TTY, si no prompt oculto. Nunca acepta valor literal.
func readKey(cmd *cobra.Command, fromStdin, noInput bool) (string, error) {
	if fromStdin || noInput {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), "API key: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		// Sin TTY real: caer a stdin.
		b2, err2 := io.ReadAll(cmd.InOrStdin())
		if err2 != nil {
			return "", err
		}
		return string(b2), nil
	}
	return string(b), nil
}
```

Run: `go get golang.org/x/term@latest` (dependencia para el prompt oculto).

Create `internal/cmd/logout.go`:
```go
package cmd

import (
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Borra las credenciales del perfil activo",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			profile := g.Profile
			if profile == "" {
				profile = "default"
			}
			store, _ := config.NewStore()
			if err := store.DeleteKey(profile); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Sesión cerrada (perfil %q).\n", profile)
			return nil
		},
	}
}
```

Create `internal/cmd/whoami.go`:
```go
package cmd

import (
	"context"
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Muestra la cuenta autenticada y el entorno (test/live)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			resp, err := cc.client.Do(context.Background(), "GET", "/v1/account", nil, nil)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			// Banner de entorno a stderr (humano); el body de account a stdout.
			if cc.format == output.Human {
				fmt.Fprintf(cmd.ErrOrStderr(), "[%s] perfil %q (key %s)\n",
					upper(cc.res.Environment), cc.res.Profile, config.RedactKey(cc.res.APIKey))
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}
}

func upper(s string) string {
	switch s {
	case "test":
		return "TEST"
	case "live":
		return "LIVE"
	default:
		return "DESCONOCIDO"
	}
}
```

- [ ] **Step 5: Implementar el comando `api`**

Create `internal/cmd/api.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

func newAPICmd() *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "api <get|post|put|delete> <path>",
		Short: "Llamada genérica a la API v1 (escape hatch)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globalsFrom(cmd)
			method := strings.ToUpper(args[0])
			path := args[1]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			// El guard --live se hereda para métodos mutadores.
			if isMutating(method) {
				if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
					return err
				}
			}
			var body []byte
			if data != "" {
				body = []byte(data)
			}
			resp, err := cc.client.Do(context.Background(), method, path, body, nil)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			if g.Verbose && resp.RequestID != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}
	cmd.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON de la petición")
	return cmd
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -v`
Expected: PASS (`TestVersionCommand`, `TestAPICommandGet`, `TestAPICommandLiveGuardBlocksMutation`).

- [ ] **Step 7: Build y smoke manual**

Run:
```bash
make build
./factuarea version
FACTUAREA_API_KEY=fact_test_xxxxxxxxxxxxxxxxxxxxxxxx ./factuarea whoami --json   # contra la API real (sandbox)
```
Expected: `version` imprime; `whoami` devuelve el account o un error con exit code coherente (`echo $?`).

- [ ] **Step 8: Commit**

```bash
git add internal/cmd go.mod go.sum
git commit -m "feat: login/logout/whoami and generic api command (end-to-end)"
```

---

## Self-Review (rellenado tras escribir el plan)

**Cobertura del spec (foundation):** runtime client (auth/retries/idempotencia/errores) ✓ T3 · exit codes incl. red/503 ✓ T2 · config/profiles + entorno por prefijo + resolución ✓ T4 · keyring + fallback transparente ✓ T5 · output json/human/plain + TTY ✓ T6 · `--live` client-side + confirmación tipada que no bloquea stdin + sandbox-only ✓ T7 · login(prompt/stdin/env, nunca literal) + logout + whoami + `api` con guard heredado + key redactada ✓ T8. *Fuera de este plan (planes 2-4):* generación de comandos, `listen`/`trigger`, `commands`/`docs`, binario/multipart, distribución.

**Placeholders:** ninguno; todo el código está completo. Único punto de limpieza explícito: eliminar la struct `env` muerta en `client/errors.go` (T3 Step 4) — es una instrucción de limpieza, no un placeholder.

**Consistencia de tipos:** `config.Store` se define en T4 y se implementa en T5; `apierr.APIError`/`TransportError` (T2) se consumen en T3/T6/exit; `output.Format` (T6) fluye por `cliContext` (T8); `GlobalFlags` (T1) se inyecta vía context y se lee con `globalsFrom` (T8). `client.Do` firma idéntica en tests (T3) y consumidores (T8).

**Dependencias añadidas:** `spf13/cobra`, `pelletier/go-toml/v2`, `zalando/go-keyring`, `golang.org/x/term`.
