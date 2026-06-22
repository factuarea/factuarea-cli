# Factuarea CLI — Documento de diseño

- **Fecha:** 2026-06-22
- **Estado:** Aprobado en diseño · endurecido con revisión adversarial multi-lente (5 lentes + verificación contra el backend real, 52 hallazgos). Pendiente de revisión humana del spec.
- **Autor:** José Luis Caravaca
- **Repo:** `github.com/factuarea/factuarea-cli` → `/Users/chelu/Personal/factuarea-cli`

> **Revisión v2:** corrige 3 supuestos load-bearing refutados contra el backend (OAuth device flow inexistente; cuerpo de `/v1/events` no byte-idéntico al webhook; `Factuarea-Version` es no-op) y endurece seguridad fiscal, generación N-nivel, override determinista, contrato de salida y distribución.

## 0. Decisiones cerradas (TL;DR)

| Decisión | Elección | Motivo |
|---|---|---|
| Lenguaje / framework | **Go + Cobra** (+ Viper) | Binario único sin runtime → máximo alcance (también usuarios no-dev "Holded"); estándar de facto en CLIs de API (Stripe, gh, Supabase, PlanetScale); arranque instantáneo; mejor distribución (GoReleaser). |
| Audiencia | **Developers + power-users / automatización**, por igual | El producto es "más Holded que Stripe": integradores **y** gestores/PYMEs/scripts/agentes IA. |
| Modelo de comandos | **Híbrido**: núcleo a mano + **CRUD generado desde `openapi.json`** pivotando en el `operationId`, override determinista por exclusión en origen | Modelo Stripe; cero drift en el momento de `make generate`; propiedad "spec-derived" de beel. |
| `listen` (webhooks→localhost) | **Fase 1: polling** de `GET /v1/events` + re-mapeo a envelope webhook + firma efímera. **Fase 2: relay WebSocket** (requiere backend). | Confirmado viable con CERO dependencia de backend; real-time como mejora. |
| Distribución | **GoReleaser** + **wrapper npm vía `optionalDependencies`** (no postinstall-download) + verificación de checksum obligatoria | `brew`/binarios/`npm i -g`/`npx` en MVP; resto de canales en Fase 2. |
| Naming | binario `factuarea`, paquete `@factuarea/cli`, repo `factuarea-cli` | Coherente con `@factuarea/sdk`, `factuarea-node`, `factuarea-php`. |
| Idioma del CLI | **es + en** en MVP; **ca** después | `es` es regla de producto; `en` es lingua franca de CLIs. |
| `login` (MVP) | **API key** por prompt oculto / stdin / env (nunca valor literal por flag). **Fase 2: OAuth 2.1 `authorization_code` + PKCE S256 + redirect a loopback** | El AS NO soporta device flow; PKCE+loopback es el patrón gh/stripe contra el AS existente. |
| `Factuarea-Version` | **No pinear por defecto**; enviar solo si el usuario lo configura | El backend valida formato pero NO altera comportamiento (no-op); pinearlo acopla el binario a un primitive inexistente. |

## 1. Contexto

Factuarea es un SaaS de facturación multi-tenant (Laravel 12 + React). Ya existe un ecosistema de developer-tooling **OpenAPI-first** que el CLI debe respetar y reutilizar:

- **API pública v1** en `https://api.factuarea.com/v1` — ~271 rutas. Ya es *Stripe-shaped*:
  - **Auth:** API key `Authorization: Bearer <key>` (o `X-API-Key`). Prefijos `fact_live_` / `fact_test_` (24 alfanum). La key está atada a `company_id`.
  - **Test/live por prefijo de key** (no hay flag ni header de entorno): `fact_test_` → empresa sandbox con efectos OFF (VeriFactu no transmite a AEAT, email no se entrega, webhooks no se entregan). **El prefijo de la key es la ÚNICA fuente de verdad del entorno.**
  - **Paginación por cursor:** `?limit=&starting_after=&ending_before=` (default 25, máx 100, cursores UUID v7 cronológicos); respuesta `{ data, has_more, next_cursor }`. `page` se rechaza con `parameter_unknown`. Algunos catálogos enum (`/v1/taxes/active`, `/v1/event-catalog`) devuelven `{data:[...]}` **sin** `has_more`/`next_cursor`.
  - **IDs:** UUID v7 opacos bajo la clave `id`; FKs como `*_id` (la global de impuestos es `taxes_id`).
  - **Sobre de error** (estilo Stripe): `{ "error": { type, code, message, subcode, param, doc_url, request_id } }`. `message` en español. Catálogo cerrado `code → [http_status, type]` (`config/public_api_error_codes.php`). **9 `type`**: `invalid_request_error` (abarca 400/405/411/413/415/422), `authentication_error` (401), `authorization_error` (403), `not_found_error` (404), `rate_limit_error` (429), `idempotency_error`, `conflict_error` (409), `api_error`, `service_unavailable_error` (5xx). El discriminador fino es `error.code`; el `type` es la clasificación gruesa.
  - **Idempotencia:** header `Idempotency-Key` (cache 24h). `idempotency_key_reused`/`in_use` (409 → suele significar "la operación YA se ejecutó con éxito"), `idempotency_key_invalid` (400).
  - **Versionado:** path `/v1` + header opcional `Factuarea-Version: YYYY-MM-DD`. **Hoy es no-op**: `ValidateFactuareaVersion` solo valida formato y persiste; no altera comportamiento ni hay catálogo de versiones.
  - **Rate-limit:** headers `X-RateLimit-{Limit,Remaining,Reset}` + `Retry-After` (en 429). `X-Request-Id` propagado/echoed.
  - **Kill-switch:** `PUBLIC_API_ENABLED=false` → 503 en `/v1/*` (respuesta **sin** envelope JSON de error).
- **OpenAPI 3.1 público y curado** servido en `GET https://api.factuarea.com/v1/openapi.json` (sin auth, cacheado 1h), con `operationId` de la forma `public-api.v1.{recurso}[.{sub}].{accion}`, request bodies tipados, ejemplos por operación, y el **scope requerido por operación** (middleware `public-api.scope:*`). **Es el artefacto que el CLI consume para generar comandos.** Gate de cobertura (`V1RouteOpenApiCoverageTest`) garantiza que cada ruta tiene `responses` y los create/update `requestBody`.
- **SDKs oficiales:** `@factuarea/sdk` (TypeScript, hey-api; su `VendorExtensionsTransformer::resolveNamespace` ya parsea el `operationId` en árbol) y `factuarea/factuarea-php` (Speakeasy). El CLI **no** reusa su código (Go), pero **reusa su algoritmo de namespacing** y replica su contrato de runtime.
- **MCP público** (233 tools, paridad 1:1 con v1) en `mcp.factuarea.com`, con **OAuth 2.1 AS**: `grant_types_supported = [authorization_code, refresh_token]`, PKCE S256, refresh con rotación, discovery RFC 8414/9728, introspect/revoke. **NO expone `device_authorization_endpoint`** (sin RFC 8628). Endpoints bajo `/api/oauth/*`, discovery en el host de la app.
- **Webhooks:** firma HMAC-SHA256, header **`Factuarea-Signature: t=<unix>,v1=<hex>`** sobre `<unix>.<rawBody>` (replay ±5 min); secret `whsec_` + 32 base62; rotación con gracia 24h (dual-sign); además headers `Factuarea-Event-Id`/`Factuarea-Event-Type`/`Factuarea-Delivery-Id`. Catálogo cerrado `<recurso>.<acción>` en `GET /v1/event-catalog`; deliveries + replay.
- **`GET /v1/events`** (recurso event, scope `events:read`): paginado por cursor UUID v7 cronológico, filtra por `type`/`livemode`. **El cuerpo del recurso event** es `{id, object:'event', type, aggregate_id, correlation_id, api_version, livemode, data, created}` — lleva `object` y `aggregate_id` de **más** frente al cuerpo del webhook `{id, type, api_version, created, livemode, correlation_id, data}`. El campo **`data` es idéntico**.

**No existe CLI todavía.** Referencias: **Stripe CLI** (Go, híbrido, `listen`/`trigger`, GoReleaser) y **beel.es CLI** (competidor español; Node/npm, "agent-first": JSON, exit codes semánticos, sandbox-default, comandos spec-derived, `beel request`, `beel docs search`).

## 2. Objetivos y no-objetivos

**Objetivos**
1. Manejar toda la API v1 desde consola, con cobertura completa sin mantenimiento manual (spec-derived).
2. DX de integración estilo Stripe: `login`, `listen`, `trigger`, escape hatch `api`.
3. **Agent-first**: salida JSON estable, discovery completo en una llamada (`commands`), exit codes semánticos — operable por un asistente IA sin `--help` intermedios.
4. **Seguro por defecto para un producto fiscal**: sandbox-default, guards explícitos, confirmación tipada en irreversibles, jamás colgar esperando stdin.
5. Instalable por el mayor número de personas (binario único + canales clave).

**No-objetivos (YAGNI)**: no reimplementar la lógica de negocio del backend; sin sistema de plugins, stack local Docker, "deploy-request workflow" ni TUI completa en MVP; **telemetría es Fase 3** (OFF por defecto, sin prompt bloqueante).

## 3. Arquitectura

### 3.1. Layout del repo

```
factuarea-cli/
├── cmd/factuarea/main.go
├── internal/
│   ├── cmd/                       # comandos a mano + registrador central
│   │   ├── root.go register.go    # register.go aplica el mapa generado + overrides
│   │   ├── login.go logout.go whoami.go config.go
│   │   ├── listen.go trigger.go api.go open.go docs.go commands.go version.go completion.go
│   │   └── override/              # overrides a mano keyed por operationId
│   ├── gen/                       # generador: openapi.json → resources_gen.go (mapa operationId→factory)
│   │   ├── gen.go templates/resource.go.tpl
│   ├── client/                    # runtime HTTP fino (auth, retries, idempotencia, paginación, errores, binario, multipart)
│   ├── config/                    # profiles, keyring, resolución de credenciales (Viper)
│   ├── output/                    # render humano/JSON/plain, tablas, exit codes, TTY/NO_COLOR, banner stderr
│   ├── webhook/                   # re-mapeo event→webhook, firma/verificación HMAC
│   └── i18n/                      # catálogos es/en (go:embed)
├── spec/openapi.json              # spec pineado y embebido (go:embed) + hash/fecha en constante generada
├── resources_gen.go               # GENERADO — no editar a mano
├── npm/                           # wrapper @factuarea/cli (optionalDependencies por plataforma + shim bin)
├── .goreleaser.yaml  Makefile  docs/
```

### 3.2. Árbol de comandos híbrido y override determinista

Dos categorías:

1. **Generados** desde `spec/openapi.json`: el generador emite un **mapa `operationId → factory de *cobra.Command`** (no registra directamente en `root`).
2. **A mano** (`internal/cmd`): núcleo de DX + **overrides** de operaciones que merecen mejor ergonomía o confirmación.

**Override por exclusión en origen (no superposición en Cobra):** un **registrador central** (`register.go`) recorre el mapa generado y, para cada `operationId`, consulta un set de overrides a mano **keyed por `operationId`**: si existe override → **salta** el generado y registra el a mano; si no → registra el generado. *(En Cobra, dos `AddCommand` con el mismo `Use` dejan resolución indefinida; por eso el override es exclusión, no "registrar después".)*

**Drift-guard de CI:** al regenerar, comparar la firma (params/flags) de cada ruta override vs la generada; **fallar/avisar** si el override no cubre params nuevos del spec (es justo el punto donde reaparece el drift que el modelo Stripe nos enseña a vigilar).

### 3.3. Pipeline de generación

- **Eje de generación = `operationId`.** El generador deriva `(grupo, [sub-recurso], accion)` parseando el contrato `public-api.v1.{recurso}[.{sub}].{accion}`, **reusando el algoritmo `resolveNamespace`/`toCamelCase` del SDK** para que CLI y SDK compartan árbol. **Invariante de build:** todo `operationId` DEBE empezar por `public-api.v1.` y tener ≥2 segmentos; si no, **el build FALLA** (no silencioso).
- **Parseo/validación del spec en build con `pb33f/libopenapi`** (OpenAPI 3.1), no `encoding/json` a mano. Test que falla si aparece una construcción de schema fuera de la allowlist soportada. El generador solo necesita `path+method+params+requestBody+operationId` y pasa el body como **JSON crudo** (no resuelve `$ref` de schemas de body).
- `make generate` baja `https://api.factuarea.com/v1/openapi.json` (host público confirmado, **no** `docs.`), lo guarda en `spec/openapi.json` (pineado + versionado en git) y ejecuta `go generate` → `resources_gen.go`.
- El binario **embebe** el spec con `go:embed` para discovery offline y `--help` rico. **Drift del embebido vs API viva:** (1) hash+fecha del spec en constante generada, expuestos en `factuarea version`; (2) `commands`/discovery operan offline contra el embebido, con `--remote` que baja el spec vivo y avisa si difiere; (3) **test CI** que baja el spec vivo y **falla si el embebido tiene drift**, forzando regeneración antes de release.
- **`Factuarea-Version`:** NO se pinea por defecto (el backend hoy no lo honra: solo valida formato y persiste). Se envía solo si el usuario lo configura (`--api-version`/config). La fecha de generación del spec es metadato informativo en `factuarea version`. **Decisión:** se abrirá un change de backend (OpenSpec) que implemente **date-versioning real** (estilo Stripe: el header pinea comportamiento; versiones desconocidas se rechazan/avisan); **cuando exista**, el CLI pasará a pinear la fecha del spec con el que se generó. Hasta entonces, no acoplar el binario al header.

### 3.4. Runtime HTTP fino (`internal/client`)

Replica el contrato de los SDKs oficiales (sin reusar código TS/PHP):
- Base URL configurable (default `https://api.factuarea.com`), prefijo `/v1`, auth `Authorization: Bearer <key>`. **Nunca** hace eco del header `Authorization` (redacción en `--verbose`/errores).
- **`Idempotency-Key`** auto (UUID) en POST de creación; override `--idempotency-key`.
- **Retries** en 429/5xx con backoff exponencial + jitter respetando `Retry-After`; máx configurable; reintenta no-idempotentes solo con la misma idempotency key.
- **Auto-paginación por cursor** (`--paginate`) usando `has_more`/`next_cursor`; **degrada a "página única"** cuando la respuesta no trae cursor (catálogos enum).
- **Respuestas binarias:** detecta `Content-Type` no-JSON (`application/pdf`, `application/zip`, `image/*`) y **no** lo parsea ni le aplica `--json`/parseo de error; expone `-o/--output <file>`; a stdout solo si stdout no es TTY (rehúsa volcar binario a la terminal con error claro). Afecta a: `*/pdf`, `payment-receipt`, `quarterly download-zip`, `products gallery/video download`, `tax-reports download`.
- **Uploads multipart:** el generador detecta `requestBody` `multipart/form-data` → flag `--file <campo>=<path>`; el runtime arma multipart real (**no** base64; la base64 es del transporte MCP, no de la API HTTP). Afecta a: `products gallery/video upload`, `purchase-invoices attach-file`.
- **Errores:** parsea el sobre **solo si `Content-Type` es JSON** → estructura tipada → exit code por `error.type`. Un body no-JSON (kill-switch 503, 5xx sin body, error de proxy) **nunca** pasa por el parseo de envelope. Eco de `X-Request-Id` también en éxito (a stderr en humano / campo en `--verbose`).

## 4. Superficie de comandos y contrato de salida

### 4.1. Comandos núcleo (a mano)

| Comando | Qué hace |
|---|---|
| `factuarea login` | API key por prompt oculto (TTY), stdin (`--api-key -`) o env. Valida contra `GET /v1/account`. **Avisa de forma muy visible** al guardar una key `fact_live_`. Fase 2: OAuth PKCE+loopback. |
| `factuarea logout` | Borra credenciales del profile. En OAuth llama además al **revoke endpoint** (`/oauth/revoke`, RFC 7009). |
| `factuarea whoami` | Cuenta/empresa, **scopes de la key**, y `environment/livemode` (derivado del prefijo) — `GET /v1/account`. |
| `factuarea config get\|set\|list` | Profiles y opciones (`~/.config/factuarea/config.toml`). |
| `factuarea listen` | Reenvía eventos webhook a un endpoint local (§6.2). |
| `factuarea trigger <evento>` | Produce eventos reales en sandbox — **guard duro anti-live** (§6.1). |
| `factuarea api get\|post\|put\|delete <path>` | Escape hatch genérico; `-d k=v`, `--paginate`. **Hereda el guard `--live`** para métodos mutadores (POST/PUT/DELETE), no queda exento. |
| `factuarea open [dashboard\|docs\|keys]` | Abre páginas web. |
| `factuarea docs search <q>` | Búsqueda local (agent-first; nunca sale de la máquina). Fuente: descripciones+ejemplos del openapi embebido (referencia de API). |
| `factuarea commands` | Manifiesto-máquina completo (discovery en 1 llamada, §4.3). |
| `factuarea version` / `completion` | Versión (+ commit + hash/fecha de spec) / completions de shell. |

### 4.2. Comandos de recursos (generados, árbol de N niveles)

`factuarea <recurso> [<sub-recurso>] <accion>`. El "17" es el conteo **top-level**; el árbol real tiene **sub-recursos** porque el spec los tiene: p.ej. `factuarea invoices payments list`, `factuarea verifactu records search`, `factuarea products gallery upload`. Recursos top-level: `invoices clients products suppliers quotes proformas delivery-notes purchase-invoices recurring-invoices series taxes tax-reports verifactu webhooks events account facturae`.

**Diccionario de naming estable:** se respetan los verbos canónicos del dominio (los del MCP/`operationId`), no se inventan. Pares confundibles documentados: `annul` (anulación AEAT) ≠ `void` (borrador), `create-corrective` ≠ `substitute`. El generador mapea `operationId → comando` de forma estable y documentada.

### 4.3. Contrato de salida (agent-first)

- **Precedencia de formato:** flag explícito > autodetección TTY. `--json` y `--plain` **mutuamente excluyentes** (combinarlos → exit 2). **Recomendación a agentes: pasar SIEMPRE `--json` explícito**, no confiar en autodetección.
- **`--json`** emite **exactamente el body de la API** (`invoices get --json` ≡ `api get /v1/invoices/<id> --json`). Para listados/`--paginate`, shape canónico: **NDJSON** (un objeto por línea) en `--paginate`, y el envelope `{data,has_more,next_cursor}` en página única. El **error en stderr** mantiene el shape del backend `error.{type,code,message,subcode,param,doc_url,request_id}`, estable también bajo `--plain`.
- **stdout = datos, stderr = mensajes/banner/errores.** El **banner TEST/LIVE** solo en salida HUMANA y a **stderr** (nunca contamina stdout/JSON); para agentes el entorno va **estructurado** (`livemode`/`environment`) en `--json` de comandos mutadores y en `whoami`.
- **Exit codes semánticos:**

  | Code | Significado | Origen |
  |---|---|---|
  | 0 | OK | — |
  | 1 | Bug inesperado del propio CLI | (reservado, no para errores de API) |
  | 2 | Uso incorrecto / guard local | flags/args inválidos, falta `--live`/`--confirm` en `--no-input`, `trigger` con key live, `--json`+`--plain` |
  | 3 | Auth | `authentication_error` (401) |
  | 4 | Permisos / plan / scope | `authorization_error` (403); o scope insuficiente detectado localmente |
  | 5 | Validación / regla de negocio | `invalid_request_error` (400/405/411/413/415/422) |
  | 6 | No encontrado | `not_found_error` (404) |
  | 7 | Rate limit (tras agotar retries) | `rate_limit_error` (429) |
  | 8 | Conflicto / idempotencia | `conflict_error` / `idempotency_error` (409) |
  | 9 | Servidor / no disponible | `api_error` / `service_unavailable_error` / 503 kill-switch / 5xx sin body |
  | 10 | **Red / timeout / sin respuesta** (transitorio, reintentable) | DNS/TLS/conexión/timeout; tras agotar retries de red |

  El exit code se deriva **siempre** de `error.type` cuando hay envelope JSON. El discriminador fino es `error.code` (el agente lo lee p.ej. para tratar `idempotency_key_reused` como "ya ejecutado con éxito").
- **`commands --json`** = manifiesto por comando: `{ path, summary, args[], flags[](name,type,required,default), required_scope, mutating, irreversible, requires_live, requires_confirm, example }`, generado del openapi embebido + metadatos de overrides. Permite operar todo el CLI con 1 llamada + N invocaciones.
- **`--help`** liderando con ejemplos; sugerencias ("did you mean"); primer byte <100ms antes de cualquier red.
- **Tabla canónica de flags globales:** `--json --plain --no-color --no-input --profile --live --idempotency-key --paginate --lang --api-version -q/--quiet -v/--verbose --confirm -o/--output --file` (semántica y precedencia documentadas en un único sitio).

## 5. Auth, config y seguridad fiscal

### 5.1. Autenticación

- **MVP — API key:** `factuarea login` por **prompt oculto** (TTY), **stdin** (`--api-key -`) o env `FACTUAREA_API_KEY`. **NUNCA** se acepta la key como valor literal de flag (visible en `ps`/`/proc/cmdline`/historial de shell/logs de CI). Guía de CI: usar el secret store del runner → `FACTUAREA_API_KEY`. La key se **redacta** en toda salida.
- **Fase 2 — OAuth 2.1 (host/discovery confirmados):** issuer `https://mcp.factuarea.com`, endpoints bajo `/api/oauth/*`, `grant_types_supported = [authorization_code, refresh_token]`, **PKCE S256** y **Dynamic Client Registration** (`/api/oauth/register`), verificado contra el well-known real. El CLI: (1) **descubre los endpoints dinámicamente vía RFC 8414** desde el issuer (nunca hardcodea rutas), (2) se **auto-registra como cliente público vía DCR** (RFC 7591), (3) `authorization_code` + PKCE S256 con **redirect a loopback `127.0.0.1:<puerto efímero>`** (el AS NO expone device flow). Scopes solicitados con mínimo privilegio.

### 5.2. Almacenamiento de credenciales

- **OS keyring** (zalando/go-keyring: Keychain/Secret Service/WinCred) con **fallback transparente** a `~/.config/factuarea/config.toml` (chmod 600) — **avisa** del fallback, nunca degrada en silencio (lección `gh`). En **Linux headless sin Secret Service** (CI) el aviso de fallback debe dispararse.
- Solo se persisten **token/key + identificadores opacos**; jamás PII de empresa.
- **OAuth (Fase 2):** access+refresh al keyring; en fallback de archivo, cifrar el refresh en reposo o chmod 600 + aviso reforzado (secreto de larga vida). **Guardado atómico** del refresh rotado (escribir-y-renombrar) + **lock por profile** para evitar refresh concurrente que dispare reuse-detection del AS. Si ocurre reuse-detection → forzar re-login con mensaje claro.

### 5.3. Profiles y resolución

- Profiles nombrados (`[default]`, `[acme-test]`, `[acme-live]`) + `--profile`; env `FACTUAREA_PROFILE`.
- **Orden de resolución:** flag > env (`FACTUAREA_API_KEY`) > profile > `[default]`.
- **El entorno (test/live) lo determina el PREFIJO de la key resuelta** — fuente única de verdad, **no** el nombre del profile. Si el entorno derivado del prefijo **contradice** el sufijo/intención del profile (p.ej. profile `*-live` resolviendo a `fact_test_`), el CLI **avisa** (y **falla** con `--no-input`). En `--json` se incluye `livemode`/`environment` derivado del prefijo para que un agente haga assert antes de mutar.

### 5.4. Seguridad para un producto fiscal

- **Sandbox por defecto:** docs y ejemplos empujan a `fact_test_`. Operar mutando en LIVE requiere key `fact_live_` **y** el flag `--live`.
- **`--live` es un guard ergonómico CLIENT-SIDE, NO una garantía.** El único control real es el prefijo de la key (por eso `login` avisa al guardar `fact_live_`). El escape hatch `api` **hereda** el guard para métodos mutadores.
- **Confirmación tipada para irreversibles** (tier severo clig.dev, espejo del `ConfirmDialog` del frontend). La lista **se deriva del spec** (por convención de verbo/acción o extensión `x-factuarea-irreversible`), **no a mano** (evita drift). Dos tiers:
  - **irreversible-fiscal** (numeración correlativa inmutable / cadena VeriFactu / AEAT): `send`/`transmit`, `assign_invoice_real_number`, `create_corrective`, `substitute_simplified`, `annul`, `archive_series`, `finalize` de tax-report. En LIVE bajo `--no-input` exigen **ambos** `--live` **y** `--confirm=<id exacto>` (sin comodín).
  - **irreversible-operacional:** `delete_*`, `void`, `cancel`, `rotate-secret`, `delete_webhook_endpoint` (corta recepción de eventos en prod), `replay_webhook_delivery` en LIVE, `activate-verifactu`, todos los `bulk_*`.
- **La confirmación NUNCA bloquea esperando stdin.** Si stdin no es TTY o `--no-input`: **fallar de inmediato** con exit 2 y stderr diciendo exactamente "pasa `--confirm=<id>`". (Un `void` colgado en un pipe mata agentes.)
- **Chequeo de scope local (MVP, modo aviso):** el generador anota el scope requerido por comando; `GET /v1/account` da los scopes de la key; antes de un comando mutador, si falta el scope → warning a stderr y **exit 4 sin llamar** (evita 403+round-trip).

## 6. Devloop de webhooks

### 6.1. `trigger <evento>` (sin backend, guard duro anti-live)

Orquesta llamadas a la API en **sandbox** para producir eventos **reales** del catálogo: `trigger invoice.paid` = crea factura sandbox + `mark-paid` (emite `invoice.created`+`invoice.paid`); análogos para `payment.received`, `quote.approved`, etc. Soporta `--override campo=valor` y encadenado de dependencias.
- **Guard duro:** `trigger` **rechaza con error (exit 2)** si la key resuelta **no** es `fact_test_`; **sin** opción `--live` para esta familia (es herramienta de devloop, sin camino a producción). Evita que un profile/env equivocado emita una factura real "para probar un webhook".

### 6.2. `listen` — Fase 1 (polling de `GET /v1/events`, sin backend)

```
factuarea listen --forward-to http://localhost:8000/api/webhooks [--events invoice.paid,quote.approved]
```

- Sondea `GET /v1/events` desde un cursor persistido, filtra por `--events`, y **reenvía** cada evento nuevo al `--forward-to`.
- **Re-mapeo de cuerpo (importante):** el feed devuelve el envelope del **recurso** (`{id, object, type, aggregate_id, correlation_id, api_version, livemode, data, created}`). El CLI **reconstruye el cuerpo de webhook** proyectando a `{id, type, api_version, created, livemode, correlation_id, data}` (fuera `object`/`aggregate_id`) **antes** de firmar/reenviar. (El campo `data` es idéntico.)
- **Firma:** el CLI imprime al arrancar (a **stderr**) un **`whsec_` efímero** estable durante la sesión, y firma cada reenvío con el mismo esquema HMAC del backend (`Factuarea-Signature: t=<unix now>,v1=...` sobre `<ts>.<rawBody>`, usando **`t=now`** para respetar la ventana antireplay ±5 min), replicando headers `Factuarea-Event-Id` (=`event.id`), `Factuarea-Event-Type` (=`event.type`) y un `Factuarea-Delivery-Id` sintético.
- **Copy correcto:** "tu mismo **código** de verificación de firma corre sin cambios; solo configuras el `whsec_` efímero que imprime el CLI en lugar del secret de producción". (Evita inducir a desactivar la verificación.)
- **`--forward-to` restringido a loopback por defecto** (defensa SSRF/exfiltración de PII del tenant: importes, NIF). Hosts remotos solo con `--allow-remote-forward` + aviso + exigir/avisar https.
- Muestra cada entrega (evento, status local, latencia); `--print-json`; reintentos al endpoint local. **Latencia:** segundos. **Dependencia de backend:** cero.

### 6.3. `listen` — Fase 2 (relay WebSocket, requiere backend)

Modelo Stripe: añadir al backend un endpoint de **"sesión CLI"** (`POST /v1/cli/sessions`) + **relay WebSocket** que empuja eventos en tiempo real; el CLI abre un WS **saliente** (sin puerto entrante) y reenvía a `--forward-to` (mismo interfaz de comando, sustituye al polling). Es trabajo de backend en el repo `factuarea`, se especificará como change OpenSpec aparte.

## 7. i18n

Catálogos `es` + `en` embebidos (`go:embed`), `ca` después. Idioma por `--lang`, env `FACTUAREA_LANG`/`LANG`, default `es`. Solo se traduce el *chrome* del CLI (help, mensajes propios, confirmaciones); los `message` del sobre de error vienen del backend (ya en español) y se muestran tal cual.

## 8. Distribución, release y calidad

- **Canales MVP (infra propia, decidido):** **GitHub Releases** (binarios + `checksums.txt` + firma **cosign keyless/OIDC** + SBOM), **Homebrew tap propio** (`factuarea/homebrew-tap`), **wrapper npm `@factuarea/cli`** (scope ya nuestro: existe `@factuarea/sdk`), e **instalador en dominio propio `get.factuarea.com/cli`** (`curl|sh`). **Acción previa:** configurar DNS/hosting de `get.factuarea.com` (no bloquea el resto del MVP; mientras tanto el `curl|sh` puede servirse desde GitHub Releases). **Fase 2:** Scoop, WinGet, deb/rpm, Docker. *(WinGet y homebrew-core tienen revisión EXTERNA → cuello de botella real, no MVP.)*
- **Wrapper npm — `optionalDependencies` por plataforma** (`@factuarea/cli-darwin-arm64`, `-linux-x64`, `-win32-x64`, …) con un `bin` que es un **shim Node** resolviendo el binario empaquetado. **No** usa postinstall-download (que falla en `--ignore-scripts`/pnpm/Yarn-PnP/`npx`); patrón **esbuild/turbo/biome** (no "@stripe/cli"; el npm oficial de Stripe es `stripe`, el SDK). `npx` se prueba en la matriz CI.
- **Verificación de checksum OBLIGATORIA en toda ruta de descarga:** el shim y el `curl|sh` verifican SHA256 contra `checksums.txt` (firmado cosign) y **abortan** si no coincide; checksum embebido en el `curl|sh`; servido versionado por HTTPS. Test que corrompe el binario y verifica que la instalación **falla**. *(Descargar un binario fiscal sin verificar checksum es un vector de supply-chain.)*
- **`curl|sh` endurecido:** toda la lógica en funciones + `main "$@"` en la última línea (evita ejecución parcial por corte de stream), `set -euo pipefail`, detección OS/arch, verificación de checksum, instala en `~/.local/bin` sin sudo por defecto, idempotente.
- **Firma de plataforma → Fase 2 (decidido).** El CLI es **dev-first** (por eso npm/`npx` y la instalación fácil); no se adquieren certs de pago en MVP. En MVP los binarios van **sin firmar**: en macOS sin fricción vía `brew`/`npm`, y para binarios sueltos se documenta el bypass (`xattr -d com.apple.quarantine`); en Windows el aviso de SmartScreen ("más info → ejecutar"). El `.goreleaser.yaml` deja **preparados los hooks de firma** (placeholders de notarización Apple Developer ID vía gon/quill y Authenticode), **activables en Fase 2** cuando se adquieran los certs y queramos pulir la experiencia para público no-dev. La integridad en MVP la cubren **cosign + checksums** (que `brew`/`npm`/`curl|sh` verifican); `cosign` no resuelve Gatekeeper/SmartScreen — eso es lo que se aplaza.
- **Completions + manpages** (Cobra) declaradas como `extra_files` en la formula brew y nfpm (deb/rpm); para binario/npm, `factuarea completion <shell>` + doc.
- **Versionado semver:** breaking change incluye cambios en la superficie de comandos generados y en el contrato JSON/exit-codes. Si el spec elimina/renombra un comando → **minor con deprecation o major, nunca patch** (el release diffea spec viejo vs nuevo y **falla** si hay borrados sin bump). Check de nueva versión (GitHub Releases latest, caché 24h, honra `DO_NOT_TRACK`/`--no-input`/CI, solo stderr, nunca en `--json`). Sin autoupdate embebido.
- **Build:** especificar `CGO_ENABLED` y estrategia (cross-compile vs matrix por-OS) por el keyring; **test de humo del keyring real** por plataforma (guardar+leer, verificar que no cae al fallback en silencio).
- **Telemetría:** **Fase 3**, OFF por defecto, sin prompt bloqueante; cuando se implemente, prompt solo en TTY interactivo y JAMÁS bajo `--no-input`/`--json`/no-TTY/CI/`DO_NOT_TRACK`.
- **Tests:** unit (Go), **golden files** de salida humana/JSON, cliente contra mock servido del spec, tests del generador (spec → comandos, incl. invariante de `operationId` y drift-guard de override), matriz CI Linux/macOS/Windows. `go vet` + `golangci-lint` + `gofumpt`.

## 9. Supuestos verificados y preguntas abiertas

**Confirmados contra el backend real:** `GET /v1/events` paginado por cursor UUID v7 cronológico (habilita `listen` Fase 1, **cero backend**) · catálogo de 9 `type` de error · paginación `{data,has_more,next_cursor}` (limit 25/máx 100, `page` rechazado) · OpenAPI 3.1 público con `operationId`+scopes+requestBody · firma HMAC reproducible client-side · generación determinista por `operationId`.

**Refutados (ya corregidos en este doc):** OAuth **device flow no existe** (→ PKCE+loopback) · cuerpo de `/v1/events` **no** byte-idéntico al webhook (→ re-mapeo) · `Factuarea-Version` **no-op** (→ no pinear).

**Decisiones tomadas (antes abiertas):**
1. **OAuth AS** — issuer `https://mcp.factuarea.com`, endpoints `/api/oauth/*`, PKCE S256 + DCR confirmados contra el well-known real → CLI usa **descubrimiento dinámico RFC 8414 + DCR + PKCE + loopback** (§5.1).
2. **Distribución** — **infra propia** (brew tap `factuarea/homebrew-tap`, npm `@factuarea/cli`, instalador `get.factuarea.com/cli`) (§8).
3. **Versionado** — abrir change de backend para **date-versioning real**; el CLI pinea cuando exista (§3.3).
4. **Firma** — **Fase 2** (CLI dev-first, sin certs de pago en MVP; pipeline preparado, bypass documentado) (§8).
5. **`payment`** — sub-recurso de invoice (`invoices payments ...`); `payment.received` documentado como evento de invoice, sin recurso top-level (§4.2).

**Acciones / dependencias residuales:**
- Configurar DNS/hosting de `get.factuarea.com` (no bloquea el MVP; fallback: servir `curl|sh` desde GitHub Releases).
- Crear repos/orgs: `github.com/factuarea/factuarea-cli` (hecho, local), `factuarea/homebrew-tap`; confirmar permisos de publicación en npm scope `@factuarea`.
- El change de backend de date-versioning se planifica y ejecuta aparte (no bloquea el MVP del CLI; solo habilita el pin posterior).

## 10. Fases (unidades de scope, no person-weeks)

- **MVP (Fase 1):** generador (operationId, libopenapi, árbol N-nivel, override determinista + drift-guard) · runtime fino (retries/idempotencia/auto-paginación tolerante/binario/multipart/exit-codes incl. red) · `login` (API key) · `config`/profiles + resolución + detección de cruce test/live · `api` (con guard) · output/exit-codes + `commands` (manifiesto) + `docs` · `trigger` (guard duro) · `listen` (polling + re-mapeo + firma + loopback) · sandbox/`--live` + confirmación tipada derivada del spec + chequeo de scope (aviso) · es/en · distribución MVP (GitHub Releases+cosign+checksums, brew tap, npm optionalDependencies) + completions/manpages · tests/golden + matriz CI.
- **Fase 2:** `login` OAuth (PKCE+loopback) + storage/rotación de refresh · `listen` WebSocket (con su change de backend) · `logs tail` si hay feed · canales scoop/winget/deb/rpm/docker · `ca` · firma de plataforma macOS/Windows.
- **Fase 3 (evaluar):** plugins · stack local · autoupdate · telemetría opt-in.

> Estimación en **scope/complejidad**, nunca en person-weeks. Cuellos reales: el change de backend para el WS (Fase 2), la verificación de los supuestos §9 (infra/firma/OAuth host), y la revisión humana del contrato de comandos generados — no el tecleo.
