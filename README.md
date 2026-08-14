# Factuarea CLI

CLI oficial de [Factuarea](https://factuarea.com) para manejar la **API pública v1** desde la terminal. Diseñado *agent-first* (salida JSON estable, exit codes semánticos, descubrimiento en una llamada) e inspirado en el CLI de Stripe.

> **Estado:** en desarrollo. Foundation, generación de comandos y devloop (`listen`/`trigger`/`docs search|list|grep|get`) completos; distribución (Homebrew/npm) en camino.

## Instalación

> Los métodos empaquetados se activan con la primera release publicada (`git tag vX.Y.Z`).

**Homebrew** (macOS/Linux):

```bash
brew install --cask factuarea/tap/factuarea
```

**npm** (cualquier plataforma con Node 20+):

```bash
npm i -g @factuarea/cli      # o: npx @factuarea/cli <comando>
```

**Instalador** (`curl | sh`, instala en `~/.local/bin`):

```bash
curl -fsSL https://github.com/factuarea/factuarea-cli/releases/latest/download/install.sh | sh
```

**Binarios** firmados (cosign) y `checksums.txt` en [Releases](https://github.com/factuarea/factuarea-cli/releases).

**Desde el código** (Go 1.26+):

```bash
git clone https://github.com/factuarea/factuarea-cli && cd factuarea-cli
make build        # genera ./factuarea
```

> La notarización (macOS) y la firma Authenticode (Windows) llegan en una fase posterior; mientras tanto, en macOS usa `brew`/`npm`, o `xattr -d com.apple.quarantine ./factuarea` para un binario suelto.

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
factuarea invoices show <uuid> --json

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

### Control horario (workforce)

Con el add-on de **control horario** activo (módulo `control_horario`), el CLI
expone los recursos de jornada, cada uno con su scope fino (`employees:*`,
`time_entries:*`, `absences:*`, `work_schedules:*`, `presence:read`,
`holidays:read`, `payroll_exports:*`): `employees`, `employee-invitations`,
`employee-seats`, `work-schedules`, `time-entries`, `time-corrections`,
`time-balances`, `time-tracking-settings`, `monthly-time-record-closes`,
`payroll-export-formats`, `absence-types`, `absence-policies`,
`absence-balances`, `absence-requests`, `absence-calendar`, `presence`,
`holidays` y `gestoria workforce-summary`.

```bash
# Fichar entrada y salida (cada asiento encadena su huella — RD-ley 8/2019)
factuarea time-entries clock-in  -d '{"employee_id":"…","source":"web"}'
factuarea time-entries clock-out -d '{"employee_id":"…","source":"web"}'

# Solicitar una ausencia y aprobarla
factuarea absence-requests create \
  -d '{"employee_id":"…","absence_type_id":"…","start_date":"2026-08-01","end_date":"2026-08-05"}'
factuarea absence-requests approve <uuid>

# Presencia del equipo en vivo
factuarea presence live --json

# Cerrar el registro mensual inalterable y exportarlo (ITSS RD-ley 8/2019)
factuarea monthly-time-record-closes create -d '{"year":2026,"month":7}'
factuarea monthly-time-record-closes export <uuid> --format rdley_8_2019 --json
```

### Automatizaciones (automations)

Con el add-on de **automatizaciones** activo (módulo `automations`, planes
`empresario` y `enterprise`), el CLI expone el motor de reglas con sus cuatro
recursos y sus scopes finos (`automations:read` en 8 comandos,
`automations:write` en 6, `automations:delete` en 1 y `automation_runs:read` en
3): el catálogo de metadatos (`automations catalog show` y
`automations catalog trigger-fields`), las reglas (`automations rules`, con su
historial en `automations rules versions`), las ejecuciones (`automations runs`,
con su detalle por paso en `automations runs steps`) y el consumo frente al
presupuesto del plan (`automations usage show`).

```bash
# Qué se puede automatizar: disparadores, operadores y acciones registradas.
factuarea automations catalog show --json
# Los campos evaluables NO están en el catálogo: son una segunda llamada, por disparador.
factuarea automations catalog trigger-fields invoice.paid --json

# Crear una regla: `actions` es una lista de objetos, así que el alta va SIEMPRE con
# el cuerpo completo (-d / --data-file). `--skeleton` imprime la plantilla en blanco.
factuarea automations rules create --skeleton
factuarea automations rules create --json -d '{
  "name": "Aviso al cobrar una factura grande",
  "scope": "empresa",
  "trigger_type": "invoice.paid",
  "conditions": {"field": "total", "operator": "gte", "value": 1000},
  "actions": [
    {"type": "notify_in_app", "order": 0, "parameters": {
      "recipient_type": "role", "role": "admin", "category": "invoice",
      "title": "Cobro grande", "message": "Se ha cobrado una factura de más de 1.000 €"
    }}
  ]
}'

# Ensayarla contra un evento de ejemplo: devuelve la traza por paso y no materializa nada.
factuarea automations rules dry-run <rule_id> --json \
  -d '{"event_type":"invoice.paid","event_payload":{"total":1200,"status":"paid"}}'

# Activarla (y pausarla cuando toque).
factuarea automations rules activate <rule_id>
factuarea automations rules pause <rule_id>

# Qué ha disparado, recorriendo el cursor de forma transparente.
factuarea automations runs list --paginate --json
factuarea automations runs list --automation_rule_id <rule_id> --status failed --json

# Una ejecución y sus pasos, con su estado y su motivo tipado.
factuarea automations runs show <run_id> --json
factuarea automations runs steps list <run_id> --json

# Relanzar. Es irreversible: sin terminal interactiva hay que confirmar con --confirm,
# y lo que se confirma es el ÚLTIMO argumento posicional (en un paso, su índice).
factuarea automations runs replay <run_id> --confirm <run_id>
factuarea automations runs steps replay <run_id> 0 --confirm 0

# Historial de versiones de la regla. Es el único listado que pagina por POSICIÓN
# (cursor numérico opaco), no por identificador.
factuarea automations rules versions list <rule_id> --json
factuarea automations rules versions show <rule_id> 2 --json

# Borrar la regla (irreversible) y mirar el consumo frente al presupuesto del plan.
factuarea automations rules delete <rule_id> --confirm <rule_id>
factuarea automations usage show --json
```

El **alcance** se elige al crear la regla y no se puede cambiar después: `empresa`
vigila sólo tu empresa, y `cartera` —para gestorías, con el módulo `gestoria`
concedido— vigila las empresas que gestionas y te entrega a ti los avisos. En
`cartera` sólo se admiten las cuatro acciones que avisan (`notify_in_app`,
`notify_channel`, `emit_webhook`, `create_calendar_event`); las que actuarían
sobre un documento de la empresa hija se rechazan al crear o editar la regla.
`factuarea automations rules list --scope cartera` las filtra, y
`factuarea automations runs list --subject_company_id <uuid>` filtra sus
ejecuciones por la empresa sobre la que actuaron.

## Devloop (webhooks)

Prueba tus webhooks en local sin desplegar ni ngrok, al estilo del CLI de Stripe:

```bash
# 1) Reenvía los eventos de tu cuenta a tu endpoint local (imprime un secret de firma efímero)
factuarea listen --forward-to http://localhost:3000/webhooks

# 2) En otra terminal, produce eventos reales en sandbox
factuarea trigger invoice.paid
factuarea trigger --list            # eventos soportados
```

`listen` sondea el feed de eventos, reconstruye el cuerpo del webhook y lo firma con HMAC (`Factuarea-Signature`) usando un secret efímero `whsec_…` que imprime al arrancar — configúralo en tu verificador y tu código de verificación corre sin cambios. Por seguridad solo reenvía a `localhost` salvo `--allow-remote-forward`. `trigger` solo opera en **sandbox** (key `fact_test_`).

Referencia rápida de la API, en local:

```bash
factuarea docs search invoice          # busca en la referencia embebida (no sale de tu máquina)

factuarea docs list                    # páginas de docs.factuarea.com: <ruta> — <título>
factuarea docs list /guides            # solo las que cuelgan de ese prefijo
factuarea docs grep "idempotency-key"  # secciones de la documentación que coinciden
factuarea docs get /guides/idempotency # la página completa, en Markdown
```

Dos fuentes distintas, ninguna con API key:

- **`docs search`** consulta el **OpenAPI embebido en el binario**: devuelve *operaciones* (comando, resumen, método y ruta). No toca la red nunca.
- **`docs list|grep|get`** consultan la **documentación publicada** (el corpus `llms-full` de [docs.factuarea.com](https://docs.factuarea.com)): devuelven *páginas y secciones* — guías, referencia de la API, catálogo de errores.

El corpus se descarga **entero y una sola vez**, se guarda en el directorio de caché del sistema (`~/Library/Caches/factuarea/docs/` en macOS, `~/.cache/factuarea/docs/` en Linux) y se filtra en local. Mientras la copia tenga menos de **15 minutos** no hay ninguna petición de red, así que una sesión que encadene `list`, `grep` y `get` descarga una vez. **Tu término de búsqueda no sale de la máquina**: la URL que se pide es fija y no depende de lo que busques.

| Opción | Qué hace |
| --- | --- |
| `--refresh` | Fuerza la descarga ignorando la copia vigente |
| `--lang en\|es\|ca` | Idioma de las guías (default `en`, el idioma fuente). La referencia de la API no se traduce y sale siempre |
| `--json` | Salida estable por stdout: `path`/`title` en `list`, `path`/`title`/`section`/`snippet` en `grep`, `path`/`title`/`markdown` en `get` |

Si la descarga falla y hay una copia en caché —aunque esté caducada—, se usa esa y se avisa por **stderr**, de modo que el JSON de stdout sigue siendo parseable; sin ninguna copia, sale con código `10` (red). La URL del corpus se puede apuntar a otro origen con `FACTUAREA_DOCS_URL`.

### Para agentes / scripting

- `factuarea commands --json` vuelca el **manifiesto completo** de comandos (path, args, flags, si muta, si es binario/paginado, ejemplo) — un asistente descubre toda la superficie en una sola llamada.
- `--json` emite el cuerpo crudo de la API por **stdout**; los errores van a **stderr** como JSON estructurado (`error.{type,code,message,request_id,doc_url}`).
- **Exit codes** semánticos: `0` ok · `2` uso/guard local · `3` auth · `4` permiso/scope · `5` validación · `6` no encontrado · `7` rate-limit · `8` conflicto/idempotencia · `9` servidor · `10` red/timeout.

## Documentación

API pública, SDKs y MCP: [docs.factuarea.com](https://docs.factuarea.com).

## Licencia

MIT.
