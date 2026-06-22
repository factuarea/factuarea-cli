# Factuarea CLI

CLI oficial de [Factuarea](https://factuarea.com) para manejar la **API pública v1** desde la terminal. Diseñado *agent-first* (salida JSON estable, exit codes semánticos, descubrimiento en una llamada) e inspirado en el CLI de Stripe.

> **Estado:** en desarrollo. Foundation + generación de comandos completos; devloop (`listen`/`trigger`) y distribución (Homebrew/npm) en camino.

## Instalación

Por ahora, desde el código (requiere Go 1.23+):

```bash
git clone https://github.com/factuarea/factuarea-cli
cd factuarea-cli
make build        # genera el binario ./factuarea
```

Próximamente: `brew install factuarea`, `npm i -g @factuarea/cli`, e instalador `curl | sh`.

## Autenticación

El CLI usa tu **API key** de Factuarea. El prefijo de la key decide el entorno:

- `fact_test_…` → **sandbox** (datos de prueba, sin efectos reales: no transmite a la AEAT, no envía email, no entrega webhooks).
- `fact_live_…` → **producción** (datos reales).

```bash
factuarea login                 # la pide por prompt oculto (nunca como argumento visible)
# o, sin interacción:
export FACTUAREA_API_KEY=fact_test_xxxxxxxxxxxxxxxxxxxxxxxx
factuarea whoami                # muestra la cuenta y el entorno (TEST/LIVE)
```

La key se guarda en el **keyring del sistema** (con fallback a `~/.config/factuarea/config.toml`, permisos 600). Soporta múltiples **perfiles** (`--profile`).

## Uso

El árbol de comandos cubre todos los recursos de la API (`factuarea <recurso> [<sub-recurso>] <acción>`), generado desde el OpenAPI (sin desincronización):

```bash
# Listar (con paginación automática por cursor)
factuarea invoices list --json
factuarea clients list --paginate --json

# Obtener uno
factuarea invoices get <uuid> --json

# Crear (cuerpo JSON por -d o --data-file)
factuarea invoices create -d '{"client_id":"…","series_id":"…","lines":[…]}'

# Acciones de dominio
factuarea invoices send <uuid>
factuarea invoices mark-paid <uuid>

# Descargas binarias (PDF/ZIP/XML)
factuarea invoices pdf <uuid> -o factura.pdf

# Subidas (multipart)
factuarea verifactu certificates upload -d '{"certificate_password":"…"}' --file-certificate_file cert.p12

# Escape hatch genérico (cualquier endpoint)
factuarea api get /v1/account --json
factuarea api post /v1/invoices -d '{…}'
```

**Operaciones en producción** (mutaciones con una key `fact_live_`) requieren el flag explícito `--live` como red de seguridad.

### Para agentes / scripting

- `factuarea commands --json` vuelca el **manifiesto completo** de comandos (path, args, flags, si muta, si es binario/paginado, ejemplo) — un asistente descubre toda la superficie en una sola llamada.
- `--json` emite el cuerpo crudo de la API por **stdout**; los errores van a **stderr** como JSON estructurado (`error.{type,code,message,request_id,doc_url}`).
- **Exit codes** semánticos: `0` ok · `2` uso/guard local · `3` auth · `4` permiso/scope · `5` validación · `6` no encontrado · `7` rate-limit · `8` conflicto/idempotencia · `9` servidor · `10` red/timeout.

## Documentación

API pública, SDKs y MCP: [docs.factuarea.com](https://docs.factuarea.com).

## Licencia

MIT.
