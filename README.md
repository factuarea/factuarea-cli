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
make dist-assets  # regenera completions/ y manpages/ desde el árbol de comandos
```

`completions/` y `manpages/` **no se versionan**: los produce `make dist-assets`
y los empaqueta la release (`.goreleaser.yaml`). El completado de línea de
órdenes es **dinámico**: los cuatro scripts (bash, zsh, fish, PowerShell) no
llevan dentro ningún nombre de comando —le preguntan al propio binario
(`factuarea __complete …`) en cada pulsación—, así que ofrecen siempre las hojas
del árbol que tengas instalado, sin volver a generarse.

> **Deuda conocida — página de manual.** El generador emite **una sola** página,
> la raíz (`manpages/factuarea.1`), y su sección *SEE ALSO* remite a **57**
> páginas por grupo (`factuarea-invoices(1)`, `factuarea-contacts(1)`…) que
> **nadie emite**: no hay página por comando. Es anterior al eje de empresa, no
> una regresión suya. Emitir el árbol entero pasaría de un fichero a varios
> cientos y cambiaría lo que publica la release, así que queda registrado aquí en
> vez de cambiarse de paso en esta entrega.

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

El árbol de comandos cubre todos los recursos de la API (`factuarea <recurso> [<sub-recurso>] <acción>`), generado desde el OpenAPI (sin desincronización). Todo recurso de una empresa cuelga del **eje de empresa** (`/v1/companies/{company}/…`), así que su comando lleva la empresa delante: como primer argumento posicional, con la flag persistente `--company`, o sin escribirla si tu credencial alcanza una sola (ver «La empresa como eje», más abajo):

```bash
# Listar (con paginación automática por cursor)
factuarea invoices list --company acme_co --json
factuarea contacts list --company acme_co --roles customer --paginate --json

# Obtener uno
factuarea invoices show --company acme_co <uuid> --json

# Crear (cuerpo JSON por -d o --data-file)
factuarea invoices create --company acme_co -d '{"client_id":"…","series_id":"…","lines":[…]}'

# Acciones de dominio
factuarea invoices send --company acme_co <uuid>
factuarea invoices mark-paid --company acme_co <uuid>

# Descargas binarias (PDF/ZIP/XML)
factuarea invoices pdf --company acme_co <uuid> -o factura.pdf

# Subidas (multipart)
factuarea verifactu certificates upload --company acme_co -d '{"certificate_password":"…"}' --file-certificate_file cert.p12

# Escape hatch genérico (cualquier endpoint): el path va LITERAL, aquí no hay
# resolución de empresa ni relleno de --company; el eje se escribe en la ruta.
factuarea api get /v1/me --json
factuarea api post /v1/companies/acme_co/invoices -d '{…}'
```

**Operaciones en producción** (mutaciones con una key `fact_live_`) requieren el flag explícito `--live` como red de seguridad.

**Clientes y proveedores son roles del mismo contacto.** No hay recursos `clients`
ni `suppliers`: se opera sobre `contacts` y se filtra o asigna el rol
(`customer`, `supplier`, `lead`) — `factuarea contacts create --name … --kind company --roles customer`,
`factuarea contacts list --roles supplier`, `factuarea contacts assign-contact-role <uuid> supplier`.
El `client_id` que piden los documentos es el UUID del contacto con rol `customer`.

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
factuarea time-entries clock-in  --company acme_co -d '{"employee_id":"…","source":"web"}'
factuarea time-entries clock-out --company acme_co -d '{"employee_id":"…","source":"web"}'

# Solicitar una ausencia y aprobarla
factuarea absence-requests create --company acme_co \
  -d '{"employee_id":"…","absence_type_id":"…","start_date":"2026-08-01","end_date":"2026-08-05"}'
factuarea absence-requests approve --company acme_co <uuid>

# Presencia del equipo en vivo
factuarea presence live --company acme_co --json

# Cerrar el registro mensual inalterable y exportarlo (ITSS RD-ley 8/2019)
factuarea monthly-time-record-closes create --company acme_co -d '{"year":2026,"month":7}'
factuarea monthly-time-record-closes export --company acme_co <uuid> --format rdley_8_2019 --json
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
factuarea automations catalog show --company acme_co --json
# Los campos evaluables NO están en el catálogo: son una segunda llamada, por disparador.
factuarea automations catalog trigger-fields --company acme_co invoice.paid --json

# Crear una regla: `actions` es una lista de objetos, así que el alta va SIEMPRE con
# el cuerpo completo (-d / --data-file). `--skeleton` imprime la plantilla en blanco
# sin llamar a la API y sin resolver la empresa; `--dry-run` compila el cuerpo y lo
# imprime con las mismas dos garantías.
factuarea automations rules create --company acme_co --skeleton
factuarea automations rules create --company acme_co --json -d '{
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
factuarea automations rules dry-run --company acme_co <rule_id> --json \
  -d '{"event_type":"invoice.paid","event_payload":{"total":1200,"status":"paid"}}'

# Activarla (y pausarla cuando toque).
factuarea automations rules activate --company acme_co <rule_id>
factuarea automations rules pause    --company acme_co <rule_id>

# Qué ha disparado, recorriendo el cursor de forma transparente.
factuarea automations runs list --company acme_co --paginate --json
factuarea automations runs list --company acme_co --automation_rule_id <rule_id> --status failed --json

# Una ejecución y sus pasos, con su estado y su motivo tipado.
factuarea automations runs show --company acme_co <run_id> --json
factuarea automations runs steps list --company acme_co <run_id> --json

# Relanzar. Es irreversible: sin terminal interactiva hay que confirmar con --confirm,
# y lo que se confirma es el recurso —el último posicional que NO es la empresa—,
# así que la empresa que aporta --company nunca es lo que se confirma.
factuarea automations runs replay --company acme_co <run_id> --confirm <run_id>
factuarea automations runs steps replay --company acme_co <run_id> 0 --confirm 0

# Historial de versiones de la regla. Es el único listado que pagina por POSICIÓN
# (cursor numérico opaco), no por identificador.
factuarea automations rules versions list --company acme_co <rule_id> --json
factuarea automations rules versions show --company acme_co <rule_id> 2 --json

# Borrar la regla (irreversible) y mirar el consumo frente al presupuesto del plan.
factuarea automations rules delete --company acme_co <rule_id> --confirm <rule_id>
factuarea automations usage show --company acme_co --json
```

El **alcance** se elige al crear la regla y no se puede cambiar después: `empresa`
vigila sólo tu empresa, y `cartera` —para gestorías, con el módulo `gestoria`
concedido— vigila las empresas que gestionas y te entrega a ti los avisos. En
`cartera` sólo se admiten las cuatro acciones que avisan (`notify_in_app`,
`notify_channel`, `emit_webhook`, `create_calendar_event`); las que actuarían
sobre un documento de la empresa hija se rechazan al crear o editar la regla.
`factuarea automations rules list --company acme_co --scope cartera` las filtra,
y `factuarea automations runs list --company acme_co --subject_company_id <uuid>`
filtra sus ejecuciones por la empresa sobre la que actuaron. En las dos, la
empresa del eje es la TUYA —la dueña de la regla—, no la empresa hija vigilada,
que viaja en `--subject_company_id`.

### La empresa como eje: el segmento `{company}` y la flag `--company`

En la v1, **toda** operación que toca datos de una empresa cuelga del mismo eje:
su ruta empieza por `/v1/companies/{company}/…`. El segmento no es un prefijo
decorativo ni un filtro opcional — es el primer tramo del recurso. Por eso
aparece en todas: la empresa deja de deducirse en silencio de la credencial y
pasa a viajar en la URL, donde se ve, se registra y se puede auditar; una clave
que alcanza dos empresas ya no puede acabar escribiendo en la que no era sin que
quede rastro de a cuál se apuntó.

De las **481** operaciones del contrato, **438** cuelgan del eje de empresa,
**38** del eje de cuenta (`/v1/accounts/{account}/…`, la cartera de una
gestoría) y **5** no cuelgan de ninguno: `/v1/me`, `/v1/event-catalog`,
`/v1/tax-catalog`, `/v1/payment-methods` y `/v1/payroll-export-formats`.

Lo que se pasa en ese segmento es el **`id` de la empresa** —el que publica
`factuarea account show` en `data.scope[].id`—, nunca su NIF ni su nombre. El
NIF aparece al lado en los mensajes de error porque es lo que tú reconoces de un
vistazo, pero lo que la ruta come es el `id`.

Ojo con el marcador: `{company}` **no siempre es el eje**. Las **8** operaciones
de `/v1/accounts/{account}/companies/{company}` —crear, activar, archivar… las
empresas de una cartera— declaran un parámetro que se llama igual pero que es el
**recurso**, no el eje. El CLI las distingue por el PATH, nunca por el nombre del
parámetro: en ellas `--company` no rellena nada y el identificador confirmado es
el de la empresa que se archiva, no el de la cuenta.

#### Forma larga y forma corta

El CLI convierte todo parámetro de ruta en argumento posicional, así que las
operaciones del eje empiezan por la empresa, y la ayuda la declara **entre
corchetes** para decir «este posicional puedes no escribirlo»:

```bash
# Forma larga: la empresa como primer argumento posicional.
factuarea invoices show acme_co inv_01931b3e --json

# La misma, con la flag persistente --company (vale en cualquier comando del árbol).
factuarea invoices show --company acme_co inv_01931b3e --json

# Forma corta: si tu credencial alcanza UNA sola empresa, no escribes la empresa.
factuarea invoices show inv_01931b3e --json
```

La ayuda larga (`--help`) de todo comando con eje termina enseñando las dos
formas y avisando de la tercera:

```
Forma canónica:  factuarea invoices show [company] <invoice>
Forma corta:     factuarea invoices show <invoice> --company <company>
Si tu credencial alcanza una sola empresa, ni siquiera hace falta --company: se resuelve sola.
```

El manifiesto para agentes publica el hueco aparte, en `optional_args`
(`{"company": "--company"}`), sin mover el orden ni los nombres de `args`:

```bash
# Cuántas operaciones llevan la empresa en la ruta, según el contrato que
# embebe TU binario (no según este README).
factuarea commands --json | jq '[.[] | select(.optional_args)] | length'
```

#### La cadena de precedencia: cuatro eslabones

De mayor a menor precedencia, el valor de empresa sale de:

1. **El argumento posicional**, si lo escribes. Gana siempre, y con él no se
   consulta nada más.
2. **La flag persistente `--company`**.
3. **La resolución automática de un solo NIF**: si no has dado ninguna de las
   dos, el CLI lee el ámbito de tu credencial (`GET /v1/me`, campo `data.scope`)
   y, **solo si alcanza una única empresa**, usa su `id`. El valor se recuerda
   durante esa invocación y **jamás se escribe en disco**: un valor persistido
   sobreviviría a la rotación de la clave o al cambio de perfil, y acabarías
   operando en silencio sobre una empresa fuera de tu ámbito.
4. **Un error accionable** si nada de lo anterior resuelve: enumera las empresas
   que alcanzas, con su `id` y su NIF, para que la orden siguiente se copie y
   pegue.

Y no hay más. **No existe `FACTUAREA_COMPANY`** ni ninguna clave de empresa en
`config.toml` —el fichero de configuración solo persiste la API key por perfil—,
y su ausencia es una decisión, no un olvido: la empresa se dice en cada
invocación o se deduce de un ámbito de una sola, pero nunca se hereda de un
estado invisible que no recuerdas haber fijado.

Aportarla **por las dos vías a la vez es un error de uso**, no una precedencia
silenciosa: el CLI no elige por ti.

#### Los errores, con su texto

Cuando la credencial alcanza varias empresas y no has dicho cuál, el mensaje
enumera el ámbito —`id` a la izquierda, que es lo que la flag come; NIF y nombre
a la derecha, que es lo que tú reconoces—:

```console
$ factuarea invoices show inv_01931b3e
Error: tu credencial alcanza 2 empresas y no elijo por ti; pasa una como argumento posicional (<company>) o fíjala con --company:
  --company cmp_01931b3e7a2c   B12345678 — Acme Sillas SL
  --company cmp_01934c8d2e5f   B87654321 — Nordic Muebles SL
```

Los otros dos fallos de la cadena:

```console
$ factuarea invoices show acme_co inv_01931b3e --company otra_co
Error: has aportado la empresa por las dos vías: como argumento posicional ("acme_co") y con la flag persistente --company ("otra_co"); quita una de las dos, no elijo por ti

$ factuarea invoices show inv_01931b3e
Error: falta la empresa: pásala como argumento posicional (<company>) o fíjala con la flag persistente --company
```

Los tres son **errores de uso**: salen por **stderr**, terminan con código **2**
y ocurren **antes de ejecutar la operación**. Lo que sí hace el tercer eslabón es
leer el ámbito de tu credencial: enumerar los NIF exige haberlos pedido, así que
el criterio es «sin llamar a la operación», no «sin tocar la red». Con `--json`
—o con stderr redirigido— el mismo mensaje sale como
`{"error":{"message":"…"}}`, para que un script no tenga que parsear texto.

#### Aridad: cuántos posicionales admite un comando con eje

En las operaciones del eje el rango de posicionales baja en uno, porque el slot
de empresa es opcional: `factuarea invoices show` acepta **1 o 2**. Si te pasas,
el error dice cuántos esperaba:

```console
$ factuarea invoices show a b c
Error: este comando acepta entre 1 y 2 argumento(s), recibió 3
```

**Aridad ambigua.** Si una operación pudiera a la vez omitir el eje y aceptar el
atajo del campo único de cuerpo como posicional, «2 argumentos» tendría dos
lecturas —«empresa omitida + campo escrito» y «empresa escrita + sin campo»—. En
este contrato **no puede pasar**: el atajo del campo único exige que la operación
no declare ningún parámetro de ruta, así que ninguna operación de eje lo tiene, y
el CLI no emite hoy ningún mensaje para ese caso. La regla queda decidida de
antemano para que nadie improvise otra el día que esa guarda se ensanche: la
invocación se **rechaza** nombrando las dos lecturas y la forma explícita de
resolver cada una (`--company <id>` para la primera, `--<campo> <valor>` para la
segunda). Lo que **nunca** se hace es adivinar por la FORMA del argumento: un
identificador de recurso tiene exactamente la misma pinta que uno de empresa, así
que el heurístico acertaría casi siempre y fallaría en silencio justo cuando te
equivocas de valor, que es cuando importa.

#### Las que exigen la empresa escrita

**11** operaciones son irreversibles y actúan sobre la empresa **entera** —su
último parámetro de ruta es el propio eje—: los `bulk-delete` de facturas,
presupuestos, proformas, albaranes, facturas de compra, productos, recurrentes y
contactos, el `bulk-archive` de contactos, la sustitución de una simplificada y
el cambio de ajustes de VeriFactu. En ellas `--company` **no rellena nada** y la
ayuda escribe el posicional sin corchetes (`<company>`), porque confirmar un
borrado masivo contra un identificador que no has tecleado no confirma nada:

```console
$ factuarea invoices bulk-delete --company acme_co
Error: esta operación es irreversible y actúa sobre la empresa ENTERA: escribe su identificador como argumento posicional (<company>). La flag persistente --company no lo rellena por defecto a propósito, porque confirmar sobre un identificador que no has escrito no confirma nada
```

El eje de **cuenta** (`/v1/accounts/{account}/…`) tampoco se rellena solo, y es a
propósito: la ergonomía del NIF único cubre a quien tiene una empresa, no a una
gestoría, que opera sobre una cartera y tiene que decir siempre sobre cuál actúa.

Las operaciones **irreversibles** del contrato (anular, sustituir, convertir,
revocar, rotar, borrar en lote…) exigen `--confirm <id del recurso>` cuando no
hay terminal interactiva, y lo que se confirma es el **recurso**, nunca la
empresa: `--company acme_co inv_01931b3e --confirm inv_01931b3e`. Con una key
`fact_live_` hacen falta además `--live`. La lista exacta, con su scope y sus
marcas, sale del propio binario:

```bash
factuarea commands --json | jq '.[] | select(.irreversible) | {command, required_scope}'
factuarea commands --json | jq '.[] | select(.binary)       | .command'
```

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
