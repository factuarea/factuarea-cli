# Factuarea CLI — Plan 3: Devloop (listen / trigger / docs)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** El devloop estilo Stripe: `listen` (reenvía eventos webhook a un endpoint local), `trigger` (produce eventos reales en sandbox) y `docs search` (referencia local). Con `listen` + `trigger` un developer integra y prueba webhooks sin desplegar nada ni usar ngrok.

**Architecture:** Comandos escritos A MANO (no generados), colgados del root junto a `login`/`whoami`/`api`. `listen` (Fase 1, sin backend) sondea `GET /v1/events`, re-mapea cada recurso `event` al cuerpo de un webhook, lo firma con un `whsec_` efímero (HMAC-SHA256, esquema del backend) y lo reenvía a `--forward-to`. `trigger` orquesta llamadas a la API en sandbox para producir eventos reales, con resolución de dependencias. `docs search` busca en el spec embebido. Reusa el runtime de Planes 1-2 (`client`, `output`, `safety`, `newCLIContext`).

**Tech Stack:** Go 1.26, stdlib (`crypto/hmac`, `crypto/sha256`, `net/http`), Cobra, más `internal/*` existentes.

## Global Constraints

- Módulo `github.com/factuarea/factuarea-cli`. Binario `factuarea`. Sin comentarios explicativos (código auto-documentado); cadenas user-facing en español.
- **Feed de eventos** (`GET /v1/events`): cursor, **oldest-first** (ascendente por tiempo). `starting_after=<id>` devuelve los más nuevos que ese id; `next_cursor` = id del último (más nuevo) de la página; `has_more` indica más páginas. Recurso `event` = `{id, object:"event", type, aggregate_id, correlation_id, api_version, livemode, data, created}`.
- **Cuerpo de webhook** (lo que el CLI reenvía) = `{id, type, api_version, created, livemode, correlation_id, data}` (SIN `object` ni `aggregate_id`; `data` idéntico).
- **Firma HMAC** (esquema del backend): header `Factuarea-Signature: t=<unix>,v1=<hex>`, sobre `<unix>.<rawBody>`, `hmac_sha256`. Secret efímero formato `whsec_` + aleatorio. Headers adicionales replicados: `Factuarea-Event-Id`, `Factuarea-Event-Type`, `Factuarea-Delivery-Id` (sintético).
- **`listen`**: firma con `t=<now>` (no el timestamp del evento) para respetar la ventana antireplay ±5min. `--forward-to` restringido a **loopback** por defecto (`127.0.0.1`/`localhost`); hosts remotos solo con `--allow-remote-forward` + aviso (se envían datos reales del tenant). Imprime el `whsec_` efímero a **stderr** al arrancar.
- **`trigger`**: **guard DURO anti-live** (`safety.RequireSandbox`, sin escapatoria `--live`); solo opera con keys `fact_test_`.
- Reusa el runtime; NO dupliques auth/retries/errores/exit-codes. Guards y exit codes del Plan 1/2.

**Out-of-scope (trackeado):** relay WebSocket de `listen` (Fase 2, requiere backend); `logs tail`; fixtures de `trigger` para los 88 eventos (solo un conjunto curado, extensible).

---

### Task 1: `internal/webhook` — firma HMAC + re-mapeo event→webhook

**Files:**
- Create: `internal/webhook/sign.go`
- Create: `internal/webhook/remap.go`
- Test: `internal/webhook/sign_test.go`, `internal/webhook/remap_test.go`

**Interfaces:**
- Produces:
  - `webhook.GenerateSecret() string` → `whsec_` + 32 chars base62-ish aleatorios.
  - `webhook.Signature(secret string, ts int64, body []byte) string` → `"t=<ts>,v1=<hex>"` con `hmac_sha256` sobre `"<ts>.<body>"`.
  - `webhook.RemapEvent(eventResource []byte) (webhookBody []byte, eventID string, eventType string, err error)` → proyecta el recurso `event` al cuerpo de webhook (quita `object`/`aggregate_id`; conserva `id,type,api_version,created,livemode,correlation_id,data`), y extrae `id`/`type` para los headers.

- [ ] **Step 1: Tests**

Create `internal/webhook/sign_test.go`:
```go
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateSecret(t *testing.T) {
	s := GenerateSecret()
	if !strings.HasPrefix(s, "whsec_") || len(s) < 20 {
		t.Fatalf("secret inválido: %q", s)
	}
	if GenerateSecret() == s {
		t.Fatal("dos secrets deben diferir")
	}
}

func TestSignatureMatchesBackendScheme(t *testing.T) {
	secret := "whsec_test"
	ts := int64(1780700000)
	body := []byte(`{"id":"x"}`)
	got := Signature(secret, ts, body)
	if !strings.HasPrefix(got, "t=1780700000,v1=") {
		t.Fatalf("formato: %q", got)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("1780700000." + string(body)))
	want := "t=1780700000,v1=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("firma:\n got %q\nwant %q", got, want)
	}
}
```

Create `internal/webhook/remap_test.go`:
```go
package webhook

import (
	"encoding/json"
	"testing"
)

func TestRemapEventStripsResourceFields(t *testing.T) {
	ev := []byte(`{"id":"evt_1","object":"event","type":"invoice.paid","aggregate_id":"agg_1","correlation_id":null,"api_version":null,"livemode":false,"data":{"invoice":{"id":"inv_1"}},"created":1780700258}`)
	body, id, typ, err := RemapEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	if id != "evt_1" || typ != "invoice.paid" {
		t.Fatalf("id/type: %q %q", id, typ)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if _, ok := m["object"]; ok {
		t.Error("object no debe estar en el cuerpo webhook")
	}
	if _, ok := m["aggregate_id"]; ok {
		t.Error("aggregate_id no debe estar")
	}
	for _, k := range []string{"id", "type", "api_version", "created", "livemode", "correlation_id", "data"} {
		if _, ok := m[k]; !ok {
			t.Errorf("falta %q en el cuerpo webhook", k)
		}
	}
}
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar**

Create `internal/webhook/sign.go`:
```go
package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func GenerateSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func Signature(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "." + string(body)))
	return "t=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
```

Create `internal/webhook/remap.go`:
```go
package webhook

import "encoding/json"

func RemapEvent(eventResource []byte) (body []byte, eventID, eventType string, err error) {
	var ev map[string]json.RawMessage
	if err = json.Unmarshal(eventResource, &ev); err != nil {
		return nil, "", "", err
	}
	_ = json.Unmarshal(ev["id"], &eventID)
	_ = json.Unmarshal(ev["type"], &eventType)
	out := map[string]json.RawMessage{}
	for _, k := range []string{"id", "type", "api_version", "created", "livemode", "correlation_id", "data"} {
		if v, ok := ev[k]; ok {
			out[k] = v
		}
	}
	body, err = json.Marshal(out)
	return body, eventID, eventType, err
}
```

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/webhook && git commit -m "feat(webhook): HMAC signing and event-to-webhook body remapping"
```

---

### Task 2: `factuarea listen` — reenvío de webhooks por polling

**Files:**
- Create: `internal/cmd/listen.go`
- Modify: `internal/cmd/root.go` (registrar `newListenCmd()`)
- Test: `internal/cmd/listen_test.go`

**Interfaces:**
- Consumes: `webhook.*`, `client.*` (acceso al feed `/v1/events`), `newCLIContext`, `output`, `safety`.
- Produces: `factuarea listen --forward-to <url> [--events a,b] [--allow-remote-forward] [--poll-interval 2s] [--print-json]`.

**Diseño del tail (load-bearing):** el feed es oldest-first; `starting_after=<id>` trae los más nuevos. Algoritmo:
1. Al arrancar, establece el **watermark** = id del evento más reciente actual (pagina con `starting_after` siguiendo `next_cursor` hasta `has_more=false`; el último id visto es el más nuevo). NO reenvía los pre-existentes.
2. Bucle de polling (cada `--poll-interval`, default 2s): `GET /v1/events?starting_after=<watermark>&limit=100`; por cada evento (en orden), re-mapea → firma con `t=now` y el `whsec_` efímero → POST a `--forward-to` con los headers; avanza el watermark al último id. Si `has_more`, sigue paginando hasta vaciar antes del siguiente sleep.
3. Filtra por `--events` (lista de tipos) si se pasó.
4. Imprime cada entrega: tipo, status code local, latencia.

- [ ] **Step 1: Test (httptest doble: API de eventos + endpoint local receptor)**

Create `internal/cmd/listen_test.go`:
```go
package cmd

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenForwardsNewEventsSigned(t *testing.T) {
	var received int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Factuarea-Signature") == "" {
			t.Error("falta Factuarea-Signature en el reenvío")
		}
		if r.Header.Get("Factuarea-Event-Type") == "" {
			t.Error("falta Factuarea-Event-Type")
		}
		atomic.AddInt32(&received, 1)
		w.WriteHeader(200)
	}))
	defer local.Close()

	var phase int32 // 0 = solo el evento viejo (watermark), 1 = aparece uno nuevo
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		after := r.URL.Query().Get("starting_after")
		if after == "" && atomic.LoadInt32(&phase) == 0 {
			_, _ = w.Write([]byte(`{"data":[{"id":"019e0000-0000-7000-8000-000000000001","object":"event","type":"client.created","data":{}}],"has_more":false,"next_cursor":"019e0000-0000-7000-8000-000000000001"}`))
			return
		}
		if atomic.LoadInt32(&phase) == 1 {
			atomic.StoreInt32(&phase, 2)
			_, _ = w.Write([]byte(`{"data":[{"id":"019e0000-0000-7000-8000-000000000002","object":"event","type":"invoice.paid","data":{"invoice":{}}}],"has_more":false,"next_cursor":"019e0000-0000-7000-8000-000000000002"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"next_cursor":null}`))
	}))
	defer api.Close()

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", api.URL)

	root := NewRootCmd()
	root.SetArgs([]string{"listen", "--forward-to", local.URL, "--poll-interval", "50ms"})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	time.Sleep(150 * time.Millisecond)
	atomic.StoreInt32(&phase, 1) // aparece el evento nuevo
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&received) != 1 {
		t.Fatalf("esperaba 1 reenvío (solo el evento nuevo), got %d", received)
	}
}
```
> El comando `listen` debe poder cancelarse: usa un `context` cancelable y soporta una vía de parada para el test (p.ej. un `--max-events` interno o un timeout). Para el test, añade un flag oculto `--exit-after <dur>` o respeta el ctx del comando. El implementador elige el mecanismo de parada testeable más limpio (recomendado: el comando para al recibir SIGINT en producción y acepta un ctx con deadline en el test).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar `listen.go`**

Estructura (el implementador completa el cuerpo respetando el diseño del tail y reusando `cc.client`):
```go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/webhook"
	"github.com/spf13/cobra"
)

func newListenCmd() *cobra.Command {
	var forwardTo, events string
	var allowRemote, printJSON bool
	var pollInterval, exitAfter time.Duration
	c := &cobra.Command{
		Use:   "listen",
		Short: "Reenvía los eventos webhook a un endpoint local (devloop)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			if err := validateForwardTo(forwardTo, allowRemote); err != nil {
				return err
			}
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			secret := webhook.GenerateSecret()
			fmt.Fprintf(cmd.ErrOrStderr(), "Reenviando a %s\nSecret de firma (configúralo en tu verificador): %s\n", forwardTo, secret)

			filter := parseEventsFilter(events)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if exitAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, exitAfter)
				defer cancel()
			}
			watermark, err := latestEventID(ctx, cc)
			if err != nil {
				return err
			}
			ticker := time.NewTicker(pollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					watermark, err = drainNewEvents(ctx, cc, watermark, filter, secret, forwardTo, printJSON, cmd)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "aviso: %v\n", err)
					}
				}
			}
		},
	}
	c.Flags().StringVar(&forwardTo, "forward-to", "", "URL local a la que reenviar los eventos (obligatorio)")
	c.Flags().StringVar(&events, "events", "", "lista de tipos a reenviar (coma-separada); vacío = todos")
	c.Flags().BoolVar(&allowRemote, "allow-remote-forward", false, "permite reenviar a hosts no-loopback (envía datos reales del tenant)")
	c.Flags().BoolVar(&printJSON, "print-json", false, "imprime el cuerpo de cada evento reenviado")
	c.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "intervalo de sondeo del feed de eventos")
	c.Flags().DurationVar(&exitAfter, "exit-after", 0, "para automáticamente tras esta duración (0 = nunca)")
	_ = c.MarkFlagRequired("forward-to")
	return c
}
```
Funciones auxiliares a implementar (mismo paquete), todas reusando `cc.client.Do(ctx, "GET", "/v1/events?...")` y `webhook`:
- `validateForwardTo(url string, allowRemote bool) error` — exige `http(s)://` y host loopback salvo `allowRemote` (devuelve `apierr.UsageError` si no, → exit 2).
- `parseEventsFilter(csv string) map[string]bool`.
- `latestEventID(ctx, cc) (string, error)` — pagina con `starting_after` siguiendo `next_cursor` hasta `has_more=false`; devuelve el id más nuevo (o "" si no hay eventos).
- `drainNewEvents(ctx, cc, watermark, filter, secret, forwardTo, printJSON, cmd) (newWatermark string, err error)` — `GET /v1/events?starting_after=<watermark>&limit=100`, por cada evento (filtrado): `webhook.RemapEvent` → `webhook.Signature(secret, now, body)` → POST a `forwardTo` con headers `Factuarea-Signature`, `Factuarea-Event-Id`, `Factuarea-Event-Type`, `Factuarea-Delivery-Id` (sintético, p.ej. `del_`+random) → log de entrega (tipo, status local, latencia). Avanza watermark; pagina si `has_more`.

El POST al endpoint local usa un `http.Client` propio (no `cc.client`, que apunta a la API), con timeout corto.

- [ ] **Step 4: Registrar en `root.go`** (`newListenCmd()` en el `AddCommand`).

- [ ] **Step 5: Run tests → PASS; build/vet/gofmt. Commit.**
```bash
git add internal/cmd/listen.go internal/cmd/listen_test.go internal/cmd/root.go
git commit -m "feat(cmd): listen — forward webhook events to a local endpoint (polling)"
```

---

### Task 3: `internal/trigger` — fixtures con resolución de dependencias

**Files:**
- Create: `internal/trigger/trigger.go`
- Create: `internal/trigger/fixtures.go`
- Test: `internal/trigger/trigger_test.go`

**Interfaces:**
- Consumes: `*client.Client` (para las llamadas a la API en sandbox).
- Produces:
  - `trigger.Supported() []string` — lista ordenada de eventos soportados.
  - `trigger.Run(ctx, c *client.Client, event string, overrides map[string]string) error` — ejecuta el fixture del evento (crea/transiciona recursos en sandbox). Error claro si el evento no está soportado (lista los que sí).
  - Helpers de dependencia: `ensureClient`, `defaultSeriesID`, `defaultTaxID` (reuse-or-create / lee defaults vía API).

**Conjunto curado inicial** (extensible — el resto se documenta como no soportado aún): `client.created`, `product.created`, `invoice.created`, `invoice.sent`, `invoice.paid`, `quote.created`, `quote.approved`.

- [ ] **Step 1: Test (httptest que simula las llamadas de creación)** — verifica que `Run` para `client.created` hace `POST /v1/clients`, y que un evento no soportado da error listando los soportados. *(El implementador escribe un httptest que cuenta las llamadas correctas por endpoint.)*

```go
package trigger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func TestRunClientCreated(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/clients") {
			posted = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cli_1"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(_ any) {}))
	if err := Run(context.Background(), c, "client.created", nil); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("client.created debe hacer POST /v1/clients")
	}
}

func TestRunUnsupported(t *testing.T) {
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	err := Run(context.Background(), c, "no.such_event", nil)
	if err == nil || !strings.Contains(err.Error(), "soportado") {
		t.Fatalf("evento no soportado debe dar error con la lista; got %v", err)
	}
}
```
> Nota: ajusta la firma de `WithSleep` en el test al tipo real (`func(time.Duration)`).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar `trigger.go` (registry) y `fixtures.go` (fixtures + deps)**

`trigger.go`:
```go
package trigger

import (
	"context"
	"fmt"
	"sort"

	"github.com/factuarea/factuarea-cli/internal/client"
)

type fixture func(ctx context.Context, c *client.Client, ov map[string]string) error

var registry = map[string]fixture{}

func Supported() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Run(ctx context.Context, c *client.Client, event string, ov map[string]string) error {
	fx, ok := registry[event]
	if !ok {
		return fmt.Errorf("evento %q no soportado por trigger. Soportados: %v", event, Supported())
	}
	return fx(ctx, c, ov)
}
```

`fixtures.go` (registra los fixtures en `init()`; cada uno orquesta llamadas con `c.Do` y resuelve deps). El implementador completa los 7 fixtures curados. Patrón de las deps:
```go
func init() {
	registry["client.created"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		_, err := c.Do(ctx, "POST", "/v1/clients", mustJSON(map[string]any{
			"name":   orDefault(ov, "name", "Cliente de prueba (trigger)"),
			"tax_id": orDefault(ov, "tax_id", "12345678Z"),
		}), nil)
		return err
	}
	registry["invoice.paid"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		inv, err := createInvoice(ctx, c, ov) // resuelve client+series+tax y crea factura
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, "POST", "/v1/invoices/"+inv+"/mark-paid", nil, nil)
		return err
	}
	// ... resto de fixtures curados
}
```
Helpers de deps (`createInvoice` reúsa o crea cliente, lee `GET /v1/series/default` para la serie por defecto, `GET /v1/taxes/active` para un tax, y crea la factura con una línea). Implementa `ensureClientID`, `defaultSeriesID`, `defaultTaxID`, `mustJSON`, `orDefault`.

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/trigger && git commit -m "feat(trigger): curated event fixtures with dependency resolution"
```

---

### Task 4: `factuarea trigger` — comando (guard sandbox duro)

**Files:**
- Create: `internal/cmd/trigger.go`
- Modify: `internal/cmd/root.go`
- Test: `internal/cmd/trigger_test.go`

**Interfaces:**
- Produces: `factuarea trigger <evento> [--override k=v ...]`; `factuarea trigger --list`.

- [ ] **Step 1: Test** — `trigger invoice.paid` con key `fact_live_` → error (sandbox-only, exit 2), sin tocar la red; `trigger --list` imprime los soportados; `trigger client.created` con key test contra httptest hace el POST.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar**
```go
package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/trigger"
	"github.com/spf13/cobra"
)

func newTriggerCmd() *cobra.Command {
	var overrides []string
	var list bool
	c := &cobra.Command{
		Use:   "trigger <evento>",
		Short: "Produce un evento real en sandbox (devloop)",
		Args:  UsageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list || len(args) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(trigger.Supported(), "\n"))
				return nil
			}
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			if err := safety.RequireSandbox(cc.res.Environment); err != nil {
				return err
			}
			ov := parseOverrides(overrides)
			if err := trigger.Run(context.Background(), cc.client, args[0], ov); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Evento %q disparado en sandbox.\n", args[0])
			return nil
		},
	}
	c.Flags().StringArrayVar(&overrides, "override", nil, "sobreescribe campos del fixture (k=v)")
	c.Flags().BoolVar(&list, "list", false, "lista los eventos soportados")
	return c
}
```
`parseOverrides([]string) map[string]string` (split por el primer `=`). Registra `newTriggerCmd()` en `root.go`.

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/cmd/trigger.go internal/cmd/trigger_test.go internal/cmd/root.go
git commit -m "feat(cmd): trigger — produce real sandbox events (sandbox-only guard)"
```

---

### Task 5: `factuarea docs search` — referencia local

**Files:**
- Create: `internal/cmd/docs.go`
- Modify: `internal/cmd/root.go`
- Test: `internal/cmd/docs_test.go`

**Interfaces:**
- Produces: `factuarea docs search <query> [--json]` — busca en el spec embebido (operationId, summary, description, path) y devuelve coincidencias `{command, summary, path, method}`. Todo local; nunca sale de la máquina.

- [ ] **Step 1: Test** — `docs search invoice` devuelve resultados que mencionan invoices; `--json` produce un array JSON.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar** — reusa `generatedOps()` (ya tiene OperationID/Action/Groups/Summary/Path) en vez de re-parsear el spec; filtra por substring (case-insensitive) de la query sobre command-path + summary + path. Salida humana (tabla) o `--json`. Registra en `root.go`.

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/cmd/docs.go internal/cmd/docs_test.go internal/cmd/root.go
git commit -m "feat(cmd): docs search over the embedded spec (local)"
```

---

### Task 6: Integración + smoke real del devloop

- [ ] **Step 1: Gate** `go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1` → verde.
- [ ] **Step 2: Smoke real (sandbox, con key del usuario)** — en una terminal: `factuarea listen --forward-to http://localhost:9999/hook --exit-after 30s` (con un receptor local simple); en otra: `factuarea trigger invoice.paid`. Verifica que el receptor recibe `invoice.created` + `invoice.paid` firmados, y que la firma valida con el `whsec_` impreso. `factuarea trigger --list` y `factuarea docs search invoice`.
- [ ] **Step 3: Actualizar README** (sección devloop: `listen`/`trigger`/`docs`). Commit.

---

## Self-Review (rellenado tras escribir el plan)

**Cobertura:** `listen` Fase 1 (polling oldest-first + watermark + re-map + firma efímera + loopback) T1-T2 · `trigger` curado con deps + guard sandbox duro T3-T4 · `docs search` local T5 · integración+smoke T6. **Fuera (trackeado):** relay WebSocket (Fase 2, backend), `logs tail`, fixtures de los 88 eventos.

**Placeholders:** los cuerpos de `listen` (auxiliares `latestEventID`/`drainNewEvents`) y los 7 fixtures de `trigger` se describen con su contrato y patrón exacto; el implementador completa el cuerpo reusando `client.Do` + `webhook.*`. Código completo en las piezas bounded (sign/remap, registry, comando trigger). NO hay lógica oculta: el diseño del tail y el patrón de fixture están especificados.

**Consistencia de tipos:** `webhook.Signature`/`RemapEvent`/`GenerateSecret` (T1) → `listen` (T2). `trigger.Run`/`Supported` (T3) → comando trigger (T4). `safety.RequireSandbox` (Plan 1) → trigger. `newCLIContext`/`UsageArgs`/`generatedOps` reusados.

**Riesgo principal:** la semántica exacta de tail del feed (oldest-first + starting_after) está verificada empíricamente contra la API real; aun así el implementador debe confirmar el comportamiento de `latestEventID` en una cuenta con muchos eventos (paginación completa al arrancar) — aceptable en Fase 1.
