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

### La empresa como eje: la flag persistente `--company`

**176** operaciones del contrato llevan la empresa en la ruta
(`/v1/companies/{company}/…`): las **164** del ERP omnicanal y las **12** de
`factuarea companies …`. El CLI convierte todo parámetro de ruta en argumento
posicional, así que esas operaciones empiezan por la empresa. Para no repetirla
en cada llamada existe la flag **persistente** `--company`, que vale en
cualquier comando del árbol:

```bash
# 1) La empresa como primer posicional (el uso de siempre; sigue funcionando igual).
factuarea sales-orders list acme_co --json

# 2) La empresa fijada con la flag persistente; el posicional se omite.
factuarea sales-orders list --company acme_co --json
factuarea sales-orders show --company acme_co so_01931b3e --json
```

No hay variable de entorno equivalente: la flag es el único camino y es
explícita en cada invocación, que es justo lo que evita operar sobre la empresa
equivocada sin enterarse.

La ayuda de cada comando con eje declara el posicional entre corchetes
(`factuarea sales-orders show [company] <sales_order> [flags]`), que es como se
lee «este posicional lo puede aportar `--company`». El manifiesto para agentes
lo publica aparte, en `optional_args` (`{"company": "--company"}`), sin mover el
orden ni los nombres de `args`:

```bash
factuarea commands --json | jq '.[] | select(.optional_args) | .command' | wc -l   # 176
```

**Aportar la empresa por las dos vías a la vez es un error de uso**, no una
precedencia silenciosa: el CLI no elige por ti. Y no aportarla por ninguna
también falla, antes de tocar la red:

```console
$ factuarea sales-orders show acme_co so_01931b3e --company otra_co
has aportado la empresa por las dos vías: como argumento posicional ("acme_co") y con la flag persistente --company ("otra_co"); quita una de las dos, no elijo por ti

$ factuarea sales-orders show so_01931b3e
falta la empresa: pásala como argumento posicional (<company>) o fíjala con la flag persistente --company
```

Dos operaciones del ERP **no** llevan eje porque cuelgan del documento de
origen, no de la empresa: `factuarea quotes convert-to-sales-order <quote>` y
`factuarea proformas convert-to-sales-order <proforma>`.

### ERP omnicanal (pedidos, compras, almacén, envíos, devoluciones y storefront)

El ERP añade **166** comandos repartidos en **44** agrupaciones. A **154** los
gatean los **seis** módulos nuevos del ERP (`backend/config/modules.php`), cada
uno con su scope fino por recurso; los **12** restantes reutilizan scope y
módulo de siempre (ver la nota bajo la tabla).

| Módulo | Planes | Agrupaciones del CLI | Scopes |
| --- | --- | --- | --- |
| `sales_orders` | `emprendedor`, `empresario`, `enterprise`, `gestionada` | `sales-orders` (+ `lines`, `buyer`, `shipping-address`), `quotes convert-to-sales-order`, `proformas convert-to-sales-order` | `sales_orders:read` · `:write` · `:transition` · `:send` · `:delete` |
| `purchase_orders` | `empresario`, `enterprise`, `gestionada` | `purchase-orders` (+ `lines`, `receipts`), `goods-receipts` (+ `lines`), `purchase-reorder-suggestions`, `purchase-invoices match` | `purchase_orders:read` · `:write` · `:transition` · `:send` · `:delete` · `goods_receipts:read` · `:write` · `:transition` |
| `warehouses` | `empresario`, `enterprise`, `gestionada` | `warehouses` (+ `locations`), `stock-transfers` (+ `lines`), `stock-reservations` | `warehouses:read` · `:write` · `:delete` · `stock_transfers:read` · `:write` · `:transition` · `stock_reservations:read` · `:write` |
| `fulfilment` | `empresario`, `enterprise`, `gestionada` | `delivery-notes fulfilment-statuses` y sus sub-recursos `picking-list`, `picking-lines`, `picking-queue`, `packages`, `shipment`, `fulfilment-status`; `carriers` | `fulfilment:read` · `:write` · `:transition` · `carriers:read` · `:write` · `:delete` |
| `returns` | `empresario`, `enterprise`, `gestionada` | `returns` (+ `corrective-candidates`) | `returns:read` · `:write` · `:transition` |
| `storefront_api` | `empresario`, `enterprise`, `gestionada` | `storefront` (`products`, `categories`, `prices`, `availability`, `sessions`, `orders`, `catalog-selections`), `storefront-keys` | `storefront:read` · `:write` (comprador anónimo, clave publicable) · `storefront_keys:read` · `:write` · `:delete` (comerciante) |

Doce comandos del ERP reutilizan scopes que ya existían y por eso los gatea su
módulo de siempre, no uno nuevo: las **seis** descargas (`sales-orders pdf`,
`purchase-orders pdf`, `stock-transfers pdf`, `delivery-notes picking-list pdf`,
`delivery-notes packing-list`, `delivery-notes shipping-label`) piden
`pdfs:read`; los **cuatro** de `stock-availability` piden `products:read`; y los
**dos** de `stores product-links` piden `stores:read`.

```bash
# --- Pedido de venta: alta, líneas, confirmación, servicio y documento -------
# `lines` es una lista de objetos: el alta va con el cuerpo completo (-d/--data-file).
factuarea sales-orders create --skeleton
factuarea sales-orders create --company acme_co --json -d '{
  "channel": "api",
  "order_date": "2026-03-12",
  "buyer_name": "Laura Giménez",
  "expected_delivery_date": "2026-03-19",
  "lines": [{"description": "Silla ergonómica Nordic", "quantity": 12, "unit_price": 45, "vat_rate": 21}]
}'

# Una línea suelta sobre el pedido ya creado (sub-recurso anidado, no aplanado).
factuarea sales-orders lines create --company acme_co so_01931b3e --quantity 2 \
  --description "Montaje e instalación" --unit-price 120 --json

# Confirmar estampa el número de serie y es IRREVERSIBLE: hay que confirmar.
factuarea sales-orders confirm --company acme_co so_01931b3e --confirm so_01931b3e

# Servir y facturar (las dos, irreversibles).
factuarea sales-orders convert-to-delivery-note --company acme_co so_01931b3e --confirm so_01931b3e
factuarea sales-orders convert-to-invoice      --company acme_co so_01931b3e --confirm so_01931b3e

# Cartera, con el cursor recorrido de forma transparente, y el PDF del pedido.
factuarea sales-orders list --company acme_co --status confirmed --paginate --json
factuarea sales-orders pdf  --company acme_co so_01931b3e -o pedido.pdf

# --- Compra: pedido al proveedor, envío, recepción y asiento de existencias --
# `lines` es lista de objetos ⇒ cuerpo completo; `--skeleton` da la plantilla.
factuarea purchase-orders create --company acme_co --json -d '{
  "supplier_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c01",
  "warehouse_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c02",
  "expected_date": "2026-03-24",
  "lines": [{"product_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c21", "quantity": 8, "unit_price": 187.5, "tax_rate": 21}]
}'
factuarea purchase-orders send --company acme_co po_0193 --confirm po_0193
factuarea purchase-orders receipts create --company acme_co po_0193 --json -d '{
  "received_on": "2026-03-18",
  "lines": [{"purchase_order_line_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c11", "quantity": 5}]
}'
# `post` asienta la recepción en el ledger de existencias: irreversible.
factuarea goods-receipts post --company acme_co gr_0193 --confirm gr_0193
# Qué reponer, y el pedido de compra que sale de ahí (la clave del lote es `items`).
factuarea purchase-reorder-suggestions list   --company acme_co --json
factuarea purchase-reorder-suggestions accept --company acme_co --json -d '{
  "items": [{"product_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c21", "offer_id": "01934c8d-2e5f-7a1b-8c9d-4e5f6a7b8c31", "quantity": 4}]
}'

# --- Almacén: altas, ubicaciones, traspasos y reservas ----------------------
# `type` es un catálogo cerrado: own, external, store, transit.
factuarea warehouses create --company acme_co --code ALM-BCN --name "Almacén Barcelona" --type own --json
factuarea warehouses locations create --company acme_co wh_0193 --code PAS-A --name "Pasillo A" --json
factuarea stock-transfers create --company acme_co --json -d '{
  "origin_warehouse_id": "wh_0193", "destination_warehouse_id": "wh_0194",
  "lines": [{"product_id": "0199aa00-0000-7000-8000-0000000000a1", "quantity": 10}]
}'
factuarea stock-transfers dispatch --company acme_co st_0193 --confirm st_0193
# En la recepción, `lines` es un MAPA «id de línea → cantidad recibida ahora».
factuarea stock-transfers receive --company acme_co st_0193 --confirm st_0193 \
  -d '{"lines": {"0199aa00-0000-7000-8000-000000000001": "10.0000"}}'
# `holder_type` NO es catálogo cerrado: lo eliges tú (minúsculas, hasta 40 car.)
# y es la mitad de la clave con la que luego consultas o liberas lo retenido.
factuarea stock-reservations create --company acme_co --json -d '{
  "holder_type": "sales_order", "holder_id": "so_01931b3e", "expires_in_seconds": 900,
  "lines": [{"product_id": "0199aa00-0000-7000-8000-0000000000a1", "quantity": 2}]
}'
factuarea stock-reservations find-by-holder --company acme_co --json -d '{"holder_type": "sales_order", "holder_id": "so_01931b3e"}'
factuarea stock-reservations release --company acme_co res_0193 --confirm res_0193
# Disponibilidad y compromisos vivos de un producto (scope `products:read`).
factuarea stock-availability show --company acme_co prod_019 --json
factuarea stock-availability commitments list --company acme_co prod_019 --json

# --- Envíos: preparación, bultos, transportista y estado de cumplimiento ----
factuarea delivery-notes picking-queue list --company acme_co --json
factuarea delivery-notes picking-list open  --company acme_co dn_0193 --confirm dn_0193
factuarea delivery-notes picking-lines pick --company acme_co dn_0193 pl_0193 --picked-quantity 12 --json
factuarea delivery-notes packages create    --company acme_co dn_0193 --reference BULTO-1/2 --weight-kg 10.5 --json
# `tracking-kind`: url_template, manual o store_provided (catálogo cerrado).
factuarea carriers create --company acme_co --name SEUR --code SEUR-24H \
  --tracking-kind url_template --tracking-url-template "https://seguimiento.example/{tracking_number}" --json
factuarea delivery-notes shipment update    --company acme_co dn_0193 --carrier-id car_0193 --json
# La transición de cumplimiento es irreversible y pide el estado destino.
factuarea delivery-notes fulfilment-statuses --company acme_co --json   # los valores admitidos
factuarea delivery-notes fulfilment-status transition --company acme_co dn_0193 \
  --target-status in_transit --confirm dn_0193
# Los tres documentos de la preparación (binarios: siempre con -o).
factuarea delivery-notes picking-list pdf --company acme_co dn_0193 -o picking.pdf
factuarea delivery-notes packing-list     --company acme_co dn_0193 -o packing.pdf
factuarea delivery-notes shipping-label   --company acme_co dn_0193 -o etiqueta.pdf

# --- Devoluciones: alta, aprobación, recepción y abono ----------------------
# `origin-type`: delivery_note, invoice o sales_order (catálogo cerrado).
factuarea returns returnable-lines --company acme_co --origin_type delivery_note --origin_id dn_0193 --json
factuarea returns create --company acme_co --json -d '{
  "origin_type": "delivery_note", "origin_id": "dn_0193", "reason": "product_defective",
  "lines": [{"origin_line_id": 8814, "quantity": 2, "reason": "product_defective"}]
}'
factuarea returns approve  --company acme_co ret_0193 --confirm ret_0193
factuarea returns receive  --company acme_co ret_0193 --confirm ret_0193
factuarea returns refund   --company acme_co ret_0193 --confirm ret_0193

# --- Storefront: credencial del comerciante y carril del comprador ----------
# La clave PUBLICABLE se emite (y se rota) con credencial de integrador.
factuarea storefront-keys scopes --company acme_co --json          # el catálogo cerrado
factuarea storefront-keys create --company acme_co --json \
  -d '{"name": "Web pública", "allowed_origins": ["https://tienda.example"], "scopes": ["storefront:read", "storefront:write"]}'
factuarea storefront-keys rotate-secret --company acme_co sk_0193 --confirm sk_0193
# El carril del comprador (catálogo → precio → disponibilidad → sesión → pedido).
factuarea storefront products list     --company acme_co --json
factuarea storefront prices resolve    --company acme_co --json -d '{"product_id": "prod_019", "quantity": 2}'
factuarea storefront availability show --company acme_co prod_019 --json
factuarea storefront sessions create   --company acme_co --json -d '{"lines": [{"product_id": "prod_019", "quantity": 2}]}'
factuarea storefront orders create     --company acme_co --json -d '{
  "session_id": "ses_0193", "marketing_consent": true,
  "buyer_name": "Ana Ruiz Molina", "buyer_email": "ana.ruiz@example.com",
  "shipping_address_line": "Calle Mayor 12, 3º B", "shipping_city": "Alicante", "shipping_country": "ES",
  "lines": [{"product_id": "prod_019", "quantity": 2}]
}'
factuarea storefront orders tracking   --company acme_co sfo_0193 --json
```

Las **41** operaciones irreversibles del ERP (confirmar, cancelar, cerrar,
asentar, despachar, recibir, abonar, revocar, rotar, borrar…) exigen `--confirm
<id del recurso>` cuando no hay terminal interactiva, y lo que se confirma es el
**recurso**, nunca la empresa: `--company acme_co so_01931b3e --confirm
so_01931b3e`. Con una key `fact_live_` hacen falta además `--live`. La lista
exacta, con su scope y sus marcas, sale del propio binario:

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
