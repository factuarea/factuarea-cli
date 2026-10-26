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

El árbol de comandos cubre todos los recursos de la API (`factuarea <recurso> [<sub-recurso>] <acción>`), generado desde el OpenAPI (sin desincronización). Todo recurso de una empresa cuelga del **eje de empresa** (`/v1/companies/{company}/…`), así que su comando lleva la empresa delante: como primer argumento posicional, con la flag persistente `--company`, o sin escribirla si tu credencial alcanza una sola (ver «La empresa como eje», más abajo). Lo que cuelga de una cuenta (`account`: `api-keys`, `invitations`, `members`, `nif-transfers`, `owner`, `webhooks`, `request-logs`, `provisioning-usage`, `usage`) lleva la cuenta con la flag `--account`, y las cuentas gestionadas (`managed-accounts`, con `imports`, `plan`, `management`, `access-level` y `claim-tokens`) tienen su propia sección más abajo («La cuenta como eje», «Cuentas gestionadas»):

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

### Escáner de documentos

El escáner conserva originales y permite revisar la extracción antes de convertirla en un **borrador de gasto**. Requiere el módulo OCR de la empresa y los scopes `purchase_invoices:read`, `purchase_invoices:write` o `purchase_invoices:delete` según la operación.

```bash
# Hasta 20 archivos PDF/JPEG/PNG; repite el flag, incluso si la ruta lleva comas.
factuarea purchase-scans create \
  --file-files factura.pdf --file-files ticket.png \
  --idempotency-key compra-lote-2026-001 --json

# La lectura es asíncrona: consulta estado, incidencias y available_actions.
factuarea purchase-scans list --paginate --json
factuarea purchase-scans show <uuid> --json
factuarea purchase-scans stats --json
factuarea purchase-scan-emails list --paginate --json

# Categorías de gasto para clasificar la conversión (purchase-invoices, no purchase-scans).
factuarea purchase-invoices expense-categories --json

# Guarda solo los cambios; sustituye expected_version por la versión observada.
factuarea purchase-scans review <uuid> --json -d '{
  "expected_version": 3,
  "fields": {"notes": {"value": "Documento revisado"}}
}'

# Convierte usando la nueva versión devuelta por review.
factuarea purchase-scans convert <uuid> \
  --expected-version 4 --idempotency-key compra-conversion-001 --json

# Original sin alterar; archivado reversible con control de versión.
factuarea purchase-scans source <uuid> -o original.pdf
factuarea purchase-scans archive <uuid> --expected-version 5 --json
factuarea purchase-scans restore <uuid> --expected-version 6 --json
```

Cada archivo admite 20 MiB y el lote 100 MiB. La respuesta `202` incluye aceptados y rechazados por índice; si todos se rechazan, el CLI conserva `data.rejected` en el error JSON y termina con código de validación. Reutiliza la misma `--idempotency-key` y el mismo orden de archivos al reintentar un lote. Las mutaciones generan una clave automáticamente si no la proporcionas.

`retry` inicia los documentos `received` cuando la lectura automática está desactivada, o reintenta fallos recuperables según `available_actions`. Una versión obsoleta produce `409`: vuelve a consultar antes de aplicar cambios. Los errores de revisión mantienen `error.details.field_errors` con los campos pendientes. `duplicate-resolution` acepta `link_existing` (con `purchase_invoice_id`) o `archive`; el override y el alta automática de proveedor requieren administrador interactivo en la aplicación.

La conversión devuelve `purchase_invoice_id`, mantiene el original adjunto y nunca emite una factura ni registra un pago. Usa importes decimales como cadenas en el JSON de revisión; los campos omitidos se conservan y `null` borra explícitamente el valor.

**Descubrimiento.** El manifiesto de `commands --json` trae un campo `operation_id` (el `operationId` del OpenAPI, p. ej. `public-api.v1.purchase_scans.create`) además del `command` de la CLI: útil para que un agente relacione un endpoint del spec con su comando sin adivinar el mapeo de nombres.

```bash
factuarea commands --json | jq '.[] | select(.operation_id | startswith("public-api.v1.purchase_scan"))'
```

### Control horario (workforce)

El módulo de **control horario** (`control_horario`) viene incluido en el plan
desde Profesional; una clave solo puede llevar sus scopes si la empresa tiene el
módulo. Con él, el CLI expone los recursos de jornada, cada uno con su scope fino (`employees:*`,
`time_entries:*`, `absences:*`, `work_schedules:*`, `presence:read`,
`holidays:read`, `payroll_exports:*`): `employees`, `employee-invitations`,
`employee-seats`, `work-schedules`, `time-entries`, `time-corrections`,
`time-balances`, `time-tracking-settings`, `monthly-time-record-closes`,
`payroll-export-formats`, `absence-types`, `absence-policies`,
`absence-balances`, `absence-requests`, `absence-calendar`, `presence`,
`holidays` y `gestoria workforce-summary`.

`employee-seats` es el add-on de facturación por empleado: no concede el módulo,
solo factura los asientos. Dar de alta o reactivar un empleado cobra su asiento
siempre que el plan factura (quedan exentos Enterprise, las empresas gestionadas
y el periodo de prueba) y, sin método de pago, responde `402`
`employee_seat_payment_method_required`.

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

Con el módulo de **automatizaciones** activo (`automations`, planes **Profesional**,
**Avanzado** y **Enterprise**; slugs `empresario`, `integral` y `enterprise`), el
CLI expone el motor de reglas con sus cuatro
recursos y sus scopes finos (`automations:read` en 8 comandos,
`automations:write` en 6, `automations:delete` en 1 y `automation_runs:read` en
3): el catálogo de metadatos (`automations catalog show` y
`automations catalog trigger-fields`), las reglas (`automations rules`, con su
historial en `automations rules versions`), las ejecuciones (`automations runs`,
con su detalle por paso en `automations runs steps`) y el consumo frente al
presupuesto del plan (`automations usage show`). Las cuotas de Enterprise se
acuerdan según contrato; consulta el consumo y los límites vigentes con ese comando.

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
vigila sólo tu empresa, y `cartera` vigila los OTROS NIF de tu cuenta y te entrega
a ti los avisos. Sólo la **titular de su cuenta** puede crearlas, y con el módulo
`gestoria` concedido (por plan o por override de empresa): un NIF adicional, sea
cual sea su origen, nunca tiene cartera propia. Lo que vigilan son los NIF reales
de la cuenta —el adscrito, uno comprado, uno traspasado— y los que otra cuenta te
cede con nivel `operate`; nunca tu propia empresa. La regla se guarda con la
empresa de la titular, sus avisos llegan a su buzón y cada ejecución lleva además
la empresa sujeto, el NIF sobre el que actuó. En `cartera` sólo se admiten las
cuatro acciones que avisan (`notify_in_app`, `notify_channel`, `emit_webhook`,
`create_calendar_event`); las que actuarían en nombre de otro NIF
(`send_document_email`, `send_payment_reminder`, `change_status`, `tag_entity`)
se rechazan al crear o editar la regla.
`factuarea automations rules list --company acme_co --scope cartera` las filtra,
y `factuarea automations runs list --company acme_co --subject_company_id <uuid>`
filtra sus ejecuciones por la empresa sobre la que actuaron (filtrar por una
empresa fuera de la cartera devuelve una lista vacía, no un error). En las dos, la
empresa del eje es la TUYA —la dueña de la regla—, no el NIF vigilado, que viaja
en `--subject_company_id`.

### La empresa como eje: el segmento `{company}` y la flag `--company`

En la v1, **toda** operación que toca datos de una empresa cuelga del mismo eje:
su ruta empieza por `/v1/companies/{company}/…`. El segmento no es un prefijo
decorativo ni un filtro opcional — es el primer tramo del recurso. Por eso
aparece en todas: la empresa deja de deducirse en silencio de la credencial y
pasa a viajar en la URL, donde se ve, se registra y se puede auditar; una clave
que alcanza dos empresas ya no puede acabar escribiendo en la que no era sin que
quede rastro de a cuál se apuntó.

De las **714** operaciones del contrato, **633** cuelgan del eje de empresa y
**64** del eje de cuenta (`/v1/accounts/{account}/…`: la cuenta propia, con su
equipo, sus NIF, sus webhooks y sus request logs). Otras **17** no son de ninguno
de los dos: **6** son las operaciones de cuentas gestionadas, donde `{account}`
es la cuenta GESTIONADA y se trata como recurso; **6** son las rutas raíz de
provisión e importación (`/v1/accounts`, `/v1/accounts/provisioning-preview` y
`/v1/accounts/imports…`), sin cuenta en la ruta; y las **5** restantes son los
catálogos y la identidad de raíz: `/v1/me`, `/v1/event-catalog`,
`/v1/tax-catalog`, `/v1/payment-methods` y `/v1/payroll-export-formats`
(6 + 6 + 5 = 17, y 633 + 64 + 17 = 714).

Lo que se pasa en ese segmento es el **`id` de la empresa** —el que publica
`factuarea account show` en `data.scope[].id`—, nunca su NIF ni su nombre. El
NIF aparece al lado en los mensajes de error porque es lo que tú reconoces de un
vistazo, pero lo que la ruta come es el `id`.

Ojo con el marcador: `{company}` **no siempre es el eje**. Las **8** operaciones
de `/v1/accounts/{account}/companies/{company}` —mostrar, activar, archivar… un
NIF de la cuenta— declaran un parámetro que se llama igual pero que es el
**recurso**, no el eje. El CLI las distingue por el PATH, nunca por el nombre del
parámetro: en ellas `--company` no rellena nada (la cuenta se nombra con
`--account`, ver más abajo) y el identificador confirmado es el de la empresa que
se archiva, no el de la cuenta.

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

`--company` no rellena nunca el hueco de la cuenta: cada eje tiene su flag.

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

### La cuenta como eje: el segmento `{account}` y la flag `--account`

Las operaciones de la cuenta propia cuelgan de `/v1/accounts/{account}/…`, con el
`id` de la cuenta (el que publica `factuarea account show --json`, es decir
`GET /v1/me`, en `data.account.id`). El hueco de la cuenta se decide por el PATH: es el primer
parámetro de ruta cuando la ruta empieza por `/accounts/{account}`. Hay dos
excepciones, y en ninguna la flag `--account` rellena nada:

- **Cuentas gestionadas (`managed-accounts`).** Ahí `{account}` es la cuenta
  GESTIONADA sobre la que actúas: es el recurso, se escribe siempre como
  argumento posicional y no hay flag ni relleno.
- **Rutas raíz de provisión e importación** (`POST|GET /v1/accounts` y
  `/v1/accounts/imports…`): no llevan cuenta en la ruta.

Para el resto, el valor de la cuenta sale de esta cadena, de mayor a menor:

1. el argumento posicional (`factuarea account members show <account> <member>`);
2. la flag persistente `--account`, que también hace opcional ese posicional;
3. solo en las **lecturas** (`GET`), la cuenta propia que publica tu credencial
   (`GET /v1/me`, `data.account.id`), que se recuerda durante la invocación y
   jamás se escribe en disco.

En una **escritura** la cuenta no se deduce: sin posicional ni `--account`, la CLI
termina con error de uso (código `2`) sin enviar la escritura, y el mensaje nombra
la cuenta propia de tu credencial y las dos formas de pasarla (para nombrarla
hace un único `GET /v1/me`, cuyo resultado no usa como destino). Una escritura no
puede ir a parar a una cuenta que no has nombrado. Aportar la cuenta por el
posicional y por la flag a la vez también es error de uso. Y en las operaciones
irreversibles cuyo último parámetro de ruta es la propia cuenta —`account
nif-transfers create`, `account nif-transfers accept` y `account owner
transfer`— ni la flag vale: hay que escribir `<account>` como posicional.
`factuarea commands --json` publica el hueco en `optional_args`
(`{"account": "--account"}`) en los comandos donde la flag lo rellena.

```bash
# 1) Una lectura resuelve la cuenta sola (GET /v1/me → GET /v1/accounts/<tu cuenta>/members)
factuarea account members list --json

# 2) Una escritura exige nombrar la cuenta: sin ella falla con código 2 y sin enviar nada;
#    con --account, la petición va a POST /v1/accounts/<account>/webhooks
factuarea account webhooks create --url https://example.com/hooks --enabled-events account.member_added
factuarea account webhooks create --account <account> --url https://example.com/hooks --enabled-events account.member_added

# 3) Una cuenta gestionada es el recurso: va siempre como posicional (GET /v1/accounts/<managed_account>);
#    --account no la rellena
factuarea managed-accounts show <managed_account> --json

# 4) Una provisión raíz no lleva cuenta (POST /v1/accounts), aunque pongas --account
factuarea managed-accounts create --external-ref cliente-0042 --plan emprendedor \
  --tax-id B12345674 --legal-name "Talleres García SL" --confirm create
```

### Cuentas gestionadas (`managed-accounts`)

Para quien provisiona NIF o cuentas de terceros. El token de `--confirm` es el
literal `create` en las operaciones raíz y el `id` del recurso en el resto:

```bash
factuarea managed-accounts list --json                          # GET  /v1/accounts
factuarea managed-accounts show <managed_account>               # GET  /v1/accounts/{account}
factuarea managed-accounts plan preview <managed_account>       # GET  /v1/accounts/{account}/plan/preview
factuarea managed-accounts plan update <managed_account> --plan empresario --confirm <managed_account>
factuarea managed-accounts access-level update <managed_account> --level view
factuarea managed-accounts claim-tokens create <managed_account> --send-email --confirm <managed_account>
factuarea managed-accounts management end <managed_account> --confirm <managed_account>
```

`managed-accounts claim-tokens create` reemite el vale de reclamación de una
cuenta GESTIONADA; no tiene relación con el vale de la cuenta propia, que se
sustituyó por el traspaso de NIF (`factuarea account nif-transfers
{create,accept,list,revoke}`, ver el CHANGELOG).

### Importación con ficheros

`managed-accounts imports` es multipart: cada fichero va con `--file-<campo>`
(`--file-accounts_file` y `--file-customers_file`) y las
opciones, por `-d` como objeto JSON plano cuyas claves son los nombres de campo
del formulario. Solo se aceptan `options[send_email]`,
`options[apply_customers_to_own_company]` y `options[confirm_plan_changes]`
(booleanas, `false` por defecto). La CLI solo comprueba que haya algún fichero;
quien exige `accounts_file` es el BACKEND (`customers_file` es opcional). Si
falta, el contrato declara para estas operaciones un 422 de validación, con el
campo inválido en `error.param`, y la CLI lo muestra tal cual.

```bash
factuarea managed-accounts imports template -o plantilla.csv    # GET /v1/accounts/imports/template
factuarea managed-accounts imports preview \
  --file-accounts_file cuentas.csv --file-customers_file clientes.csv   # no escribe
factuarea managed-accounts imports create \
  --file-accounts_file cuentas.csv --file-customers_file clientes.csv \
  -d '{"options[send_email]":true}' --confirm create            # irreversible
```

### Webhooks y request logs de cuenta

Los endpoints de webhook de la cuenta (`account webhooks`, con sus entregas en
`account webhooks deliveries`) y los registros de peticiones de la cuenta
(`account request-logs`) usan el eje de cuenta de arriba: lecturas sin cuenta se
deducen, escrituras la piden.

```bash
factuarea account webhooks list --json
factuarea account webhooks create --account <account> --url https://example.com/hooks \
  --enabled-events account.member_added --account-relationship own
factuarea account webhooks deliveries list <webhook> --status failed --json
factuarea account webhooks deliveries retry --account <account> <webhook> <delivery>
factuarea account request-logs list --only_errors true --json
factuarea account request-logs show <request_id> --json
```

### Tareas y proyectos (tasks)

Con el módulo **tareas** (`tasks`, incluido en todos los planes), el CLI expone los proyectos con su tablero de columnas, las tareas, las etiquetas, el tiempo imputado y la agenda: 80 comandos en siete grupos, cada uno con su scope fino. Los scopes son `projects:read|write|delete`, `tasks:read|write|delete`, `users:read` y `notifications:read|write`; la factura de horas de un proyecto (`projects time-invoices create`) pide además `invoices:write`, porque crea una factura.

| Grupo | Qué cubre |
| --- | --- |
| `projects` | proyectos, columnas (`projects columns`), campos personalizados, exportar e importar tareas (`projects tasks`), resumen de tiempo y factura de horas |
| `tasks` | tareas y sus subrecursos: comentarios, adjuntos, entradas de tiempo (`tasks time-entries`), etiquetas, relaciones, vínculos con documentos y contactos, actividad |
| `task-labels` | etiquetas de tarea de la empresa |
| `task-timers` | el temporizador en marcha del titular de la key |
| `users` | miembros de la empresa a los que se puede asignar una tarea |
| `notifications` | avisos del titular de la key |
| `agenda` | agenda combinada: tareas con vencimiento, vencimientos de documentos, plazos fiscales y ausencias |

```bash
# Un proyecto: su clave (una letra y hasta nueve letras o números) prefija la de sus tareas (WEB-1, WEB-2…).
factuarea projects create --name "Web corporativa" --key WEB --json
factuarea projects columns list <project_id> --json

# Crear una tarea. El cuerpo completo permite, además, vincularla a un documento o contacto
# en la misma petición con `entity_link` (invoice, quote, contact, product…).
factuarea tasks create --json -d '{
  "project_id": "<project_id>",
  "title": "Preparar el presupuesto",
  "priority": "high",
  "due_on": "2026-10-15",
  "entity_link": {"type": "quote", "id": "<quote_id>"}
}'

# Los comandos piden el UUID; con la clave de la tarea se resuelve primero.
factuarea tasks find-by-key WEB-1 --json

# Cambiar su estado: a una columna del proyecto o a un estado virtual (`planned` es el backlog).
factuarea tasks status <task_id> --column-id <column_id>
factuarea tasks status <task_id> --status planned

# Buscar y recorrer el cursor de forma transparente.
factuarea tasks search --project_id <project_id> --status active --paginate --json

# Imputar tiempo: un periodo cerrado (ISO 8601 con zona horaria)…
factuarea tasks time-entries create <task_id> \
  --started-at 2026-09-28T09:00:00+02:00 --ended-at 2026-09-28T10:30:00+02:00 \
  --description "Revisión del presupuesto"

# …o con el temporizador del titular de la key, que solo admite uno en marcha.
factuarea tasks timer start <task_id>
factuarea task-timers current --json      # `{"data": null}` si no hay ninguno
factuarea task-timers stop --json

# Borrar una columna con tareas exige decir dónde van. Es irreversible: sin terminal
# interactiva hay que confirmar con --confirm, y lo que se confirma es el ÚLTIMO
# argumento posicional (aquí, la columna borrada).
factuarea projects columns delete <project_id> <column_id> \
  --move_to_column_id <other_column_id> --confirm <column_id>
```

Los borrados de proyectos, columnas, campos personalizados, etiquetas, tareas (también `tasks bulk-delete`), comentarios, adjuntos y entradas de tiempo son irreversibles y piden `--confirm`; las mutaciones generan la cabecera `Idempotency-Key` si no pasas `--idempotency-key`.

**`task-timers` no es el fichaje de jornada.** El temporizador y las entradas de `tasks time-entries` registran el tiempo dedicado a una tarea, para medirlo y facturarlo; no son el registro de jornada del módulo de control horario (`factuarea time-entries clock-in`, arriba), que tiene su propio scope y su propio registro inalterable.

### Plantillas PDF y presentación de documentos

Los comandos salen del OpenAPI, igual que el resto, y todos cuelgan del eje de
empresa (`/v1/companies/{company}/…`): la empresa va como primer argumento
posicional, con `--company` o, si tu credencial alcanza una sola, se resuelve
sola (ver «La empresa como eje»). Los nuevos, por grupo:

| Grupo | Comandos |
| --- | --- |
| `pdf-templates` | `list`, `create`, `find`, `update`, `duplicate`, `archive`, `restore`, `impact`, `publish-revision`, `revision`, `defaults-get`/`defaults-set`, `contact-default-get`/`contact-default-set`, `asset-upload`, `asset-get` |
| `pdf-commercial-identities` | `list`, `create`, `find`, `update`, `archive`, `restore`, `impact`, `publish-revision`, `revision` |
| `pdf-template-previews` | `create` |
| `pdf-artifacts` | `prepare`, `render`, `show`, `download` |
| `invoices`, `quotes`, `proformas`, `delivery-notes` | `get-presentation`, `assign-presentation`, `list-presentations`, `revise-presentation` |
| `invoices administrative-revisions` | `index`, `store` |
| `series` | `get-pdf-template`, `assign-pdf-template` |

`series update` (PATCH) acepta además `--pdf-template-id` o `--null-pdf-template-id`
(quita la plantilla de la serie), siempre acompañados de
`--pdf-template-expected-version`.

`quotes accept` y `proformas accept` aceptan la presentación revisada cuando la
empresa tiene activa la presentación con revisiones (si no, se ignora):

- `--presentation-precondition.<campo>` (los siete campos) es obligatorio en la
  primera captura: un documento sin captura retenida o editado después de
  capturarlo. Sale de una vista previa `persisted_draft` (`pdf-template-previews
  create`), con `purpose` `complete` y `format` `a4`. Sin él, `428
  pdf_presentation_precondition_required` y no se escribe nada.
- `--expected-presentation-uuid` es opcional y cita la captura retenida que has
  revisado; si ya no es la vigente, `409 pdf_presentation_conflict` y nada cambia.

```bash
factuarea quotes accept acme_co <quote> --expected-presentation-uuid <presentation_uuid> --json
```

```bash
factuarea pdf-templates create --company acme_co --name "Factura clásica" --base-preset classic --json
factuarea pdf-templates asset-upload --company acme_co --file-file logo.png --data '{"kind":"logo"}' --json
factuarea pdf-templates asset-get acme_co <asset_id> -o logo.png
factuarea invoices assign-presentation acme_co <uuid> --null-template-id --expected-version 1 --json
factuarea invoices list-presentations acme_co <uuid> --page 1 --per_page 20 --json
factuarea delivery-notes get-presentation acme_co <uuid> --json
```

`examples/document-templates.sh` recorre la receta completa contra un servidor de pruebas (`FACTUAREA_BASE_URL`), sin cuentas reales.

**Cabeceras condicionales.** Las cabeceras que una operación declara en el spec son flags literales (`--if-match`, `--if-none-match`): el valor viaja tal cual, con sus comillas, y un valor vacío es un error de uso. `--show-etag` (en las operaciones que devuelven `ETag`) escribe `etag: <valor>` en **stderr** y no cambia stdout, de modo que se puede encadenar: `ETAG=$(factuarea pdf-templates find --company acme_co <id> --show-etag --json 2>&1 >/dev/null | sed -n 's/^etag: //p')`. Un `If-Match` obsoleto responde `412`; una versión (`expected_version`) obsoleta, `409`; sin ninguna precondición, `428`. El CLI no rellena versiones ni ETags por ti.

**Presencia de campos.** El cuerpo distingue entre campo omitido (no se envía), `null`, vacío (`""`, `[]`), `0` y `false`: todos se envían tal cual si los pasas. Para `null` usa `--null-<campo>` (solo en campos que admiten null) o `null` en `-d`/`--data-file`; un campo requerido y nullable (como `template_id`) se satisface con `--null-template-id`. Los números de `-d`/`--data-file` conservan su literal.

**Descargas binarias.** Una operación binaria (`pdf-artifacts download`, `invoices facturae`, `invoices pdf`…) escribe los bytes exactos a `-o` (PDF, XML, imagen, ZIP):

- `200`: bytes a `-o` (o a stdout si no es un terminal).
- `202`: aún no está listo; se imprime el JSON de estado y `retry-after: <s>` en stderr, y **no** se crea el fichero. Repite la llamada pasado ese tiempo.
- `304` (con `--if-none-match`): éxito, sin salida y sin tocar el fichero existente.
- `pdf-templates asset-get` es dual: según el `Content-Type` de la respuesta escribe la imagen a `-o` o imprime el JSON.
- Los errores `429`/`503` exponen `retry_after` en el JSON de stderr.

**Paginación por operación.** Solo las operaciones que declaran `starting_after` ofrecen `--paginate` (cursor, NDJSON). Los listados con `page`/`per_page` (plantillas, identidades, historial de presentaciones) hacen **una** petición por llamada; pasa `--page` y `--per_page` tú.

**FACe sin reintento.** `invoices face-submissions submit` y `face-submissions cancel` hacen una sola llamada: ante `429`, `5xx` o un corte de conexión no se repiten, porque una transmisión incierta no debe duplicarse a ciegas. Si falla, consulta `invoices face-submissions list` / `face-submissions show` antes de volver a enviar. El resto de operaciones reintenta `429`/`5xx` conservando el mismo cuerpo y la misma `Idempotency-Key`, y respetando `Retry-After`.

### Sincronizar el spec desde un fichero

El spec embebido (`internal/spec/openapi.json`) se puede tomar de un fichero ya exportado, **copiándolo byte a byte**: sin normalizar ni reserializar, para que su sha256 sea el del origen.

```bash
make generate-from-file SPEC_FILE=/ruta/al/openapi.json   # cp + go run internal/gen/main.go
sha256sum internal/spec/openapi.json                       # debe coincidir con el del origen
factuarea version                                          # muestra los primeros 12 hex del hash embebido
```

`make generate` y `make generate-dev` sí normalizan (indentan y añaden salto final), porque su fuente es JSON compacto. Tras regenerar, adopta solo `internal/cmd/resources_gen.go`; no se edita a mano.

### Documentos de control del transporte (DeCA)

Con el módulo **documentos de control** (`transport_documents`, desde el plan Profesional y junto al de albaranes), el CLI expone el documento electrónico de control administrativo (DeCA) del transporte de mercancías por carretera: 17 comandos bajo `factuarea transport-documents`. Los scopes son `transport_documents:read|write|transition|delete`; el PDF de una versión pide `pdfs:read`, y el justificante de emisión `transport_documents:read`, porque contiene el registro de emisión.

```bash
# Alta en borrador a partir de albaranes (un envío por albarán) y emisión con el siguiente número de la serie DC.
factuarea transport-documents create --company acme_co --json -d '{
  "company_role": "shipper",
  "carrier": {"contact_id": "<contact_id>"},
  "delivery_note_ids": ["<delivery_note_id>"],
  "vehicle": {"tractor_plate": "1234BCD"}
}'
factuarea transport-documents issue --company acme_co <id> --confirm <id>

# Listar y filtrar (el listado se llama `index`, como su operación en la API).
factuarea transport-documents index --company acme_co --status issued --company_role shipper --paginate --json

# Modificar durante el servicio: mismo número, misma URL y mismo QR.
factuarea transport-documents modify --company acme_co <id> --reason "Cambio de matrícula" --data-origin company \
  --vehicle.tractor-plate 5678FGH --confirm <id>

# Descargas: el PDF (versión actual o --version N), el QR (svg o png) y el justificante de emisión.
factuarea transport-documents pdf --company acme_co <id> -o deca.pdf
factuarea transport-documents qr --company acme_co <id> --format png -o deca-qr.png
factuarea transport-documents issuance-proof --company acme_co <id> -o justificante.pdf
factuarea transport-documents evidence --company acme_co <id> --json      # el registro de emisión, en JSON
```

Emitir (`issue`), modificar (`modify`), anular (`cancel`), sustituir (`supersede`) y enviar (`send`) son irreversibles: sin terminal interactiva hay que confirmarlos con `--confirm <id>`. Cerrar (`close`) y reabrir (`reopen`) no piden confirmación, y borrar (`delete`) solo admite borradores.

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

To discover expense categories for scanner review, run `factuarea purchase-invoices expense-categories`. Use the returned `id` in the `expense_category` field.
