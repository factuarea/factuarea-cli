# Changelog

Todo cambio reseñable de la CLI de Factuarea se anota aquí. El formato sigue
[Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y el versionado,
[SemVer](https://semver.org/lang/es/): mientras la CLI está en `0.x` una rotura
viaja en una `minor`, y `1.0.0` queda reservado para la GA de la API pública
(ver [`docs/RELEASING.md`](docs/RELEASING.md)).

Este fichero arranca con la ventana de rotura del eje de empresa; las etiquetas
anteriores (hasta `v0.4.0`) no tienen entrada aquí.

## [Unreleased]

## [0.10.0] - 2026-10-26

`internal/spec/openapi.json` embebe el contrato v1 de **714 operaciones** en
**572 paths** (sha256 `0219a93b…bf14`), y `internal/cmd/resources_gen.go` se
ha regenerado entero desde él: **50 comandos nuevos**, todos en el eje de
empresa, y ninguno retirado, renombrado ni movido de ruta. El árbol queda en
633 operaciones del eje de empresa, 64 del eje de cuenta, 6 de cuentas
gestionadas, 6 rutas raíz de provisión e importación y 5 catálogos e identidad
de raíz.

### Añadido

- **Plantillas PDF y presentación de documentos.** Los 50 comandos nuevos
  cuelgan de `/v1/companies/{company}/…`, así que llevan la empresa como el
  resto del árbol (posicional, `--company` o resolución automática):

  | Grupo | Comandos |
  | --- | --- |
  | `pdf-templates` (16) | `list`, `create`, `find`, `update`, `duplicate`, `archive`, `restore`, `impact`, `publish-revision`, `revision`, `defaults-get`/`defaults-set`, `contact-default-get`/`contact-default-set`, `asset-upload`, `asset-get` |
  | `pdf-commercial-identities` (9) | `list`, `create`, `find`, `update`, `archive`, `restore`, `impact`, `publish-revision`, `revision` |
  | `pdf-template-previews` (1) | `create` |
  | `pdf-artifacts` (4) | `prepare`, `render`, `show`, `download` |
  | `invoices`, `quotes`, `proformas`, `delivery-notes` (16) | `get-presentation`, `assign-presentation`, `list-presentations`, `revise-presentation` |
  | `invoices administrative-revisions` (2) | `index`, `store` |
  | `series` (2) | `get-pdf-template`, `assign-pdf-template` |

  ```bash
  factuarea pdf-templates list acme_co --json
  factuarea invoices assign-presentation acme_co <invoice> --template-id <template> --expected-version 1 --json
  factuarea pdf-artifacts download --company acme_co <artifact> -o factura.pdf
  ```

  La marca de una identidad comercial (`pdf-commercial-identities create` y
  `publish-revision`) admite una versión para fondo oscuro y un tamaño:
  `--configuration.logo-dark-asset-id`,
  `--configuration.brand-image-dark-asset-id`,
  `--configuration.dark-background-fallback` (`silhouette`, por defecto, o
  `plate`) y `--configuration.brand-size` (`small`, `medium` o `large`). En la
  configuración de una plantilla, `branding.size` admite `inherit`, que toma el
  tamaño de la identidad; va en el cuerpo (`-d`/`--data-file`), igual que en la
  identidad sin guardar de `pdf-template-previews create`, que acepta los
  mismos campos.

  `series update` acepta además `--pdf-template-id` o `--null-pdf-template-id`,
  con `--pdf-template-expected-version`. La receta completa está en
  `examples/document-templates.sh`.

- **Aceptar con la presentación revisada.** `quotes accept` y `proformas accept`
  ganan `--presentation-precondition.<campo>` (los siete campos de una vista
  previa `persisted_draft`, obligatorios en la primera captura cuando la
  presentación con revisiones está activa; sin ellos, `428`) y
  `--expected-presentation-uuid` (opcional: cita la captura retenida que has
  revisado; si ya no es la vigente, `409 pdf_presentation_conflict` y nada
  cambia). Con la presentación con revisiones desactivada, se ignoran.

- **Comisiones de Stripe como gasto** (cambio del contrato que la API pública
  sirve desde su v1.17.2, posterior a la v0.9.0 de la CLI).
  `stripe-autoinvoicing accounts update` gana `--fee-expenses-enabled`,
  `--fee-expenses-tax-treatment` (`reverse_charge_eu` o `exempt`) y
  `--fee-expenses-since` (`YYYY-MM`). Activarlo exige el módulo de compras del
  plan y, en la misma petición o ya guardados, el tratamiento y el mes de
  inicio. La cuenta conectada informa además de esos campos y de
  `connection_method` `platform` (la cuenta de Stripe propia), y la composición
  de los payouts gana `refunds_total`, `standalone_fee_total` y `other_total`.

- **Cabeceras condicionales.** Las cabeceras que una operación declara en el
  contrato son flags literales: `--if-match` e `--if-none-match` (esta última
  también cuando la operación declara un `304`). El valor viaja tal cual, con
  sus comillas, y un valor vacío es un error de uso. `--show-etag` escribe
  `etag: <valor>` en stderr sin tocar stdout.

- **Null explícito.** `--null-<campo>` envía `null` en los campos que lo
  admiten. Omitido, `null`, `""`, `[]`, `0` y `false` se distinguen también en
  `-d`/`--data-file`, y los números conservan su literal.

- **Descargas con estado.** Un `200` escribe los bytes exactos en `-o`; un
  `202` imprime el JSON de estado y `retry-after` en stderr sin crear el
  fichero; un `304` es éxito sin salida y sin tocar el fichero existente.
  `pdf-templates asset-get` es dual: según el `Content-Type` de la respuesta
  escribe la imagen o imprime el JSON. Los errores `429`/`503` exponen
  `retry_after`.

### Cambiado

- **Contrato resincronizado con el tipo de plantilla.** Las mismas 714
  operaciones y 572 paths, sin comandos nuevos ni retirados. Las plantillas PDF
  ganan `document_kind` (`commercial` o `transport-document`): `pdf-templates
  create` lo acepta, `list` lo ofrece como filtro y las respuestas lo devuelven.
  Una plantilla nunca cambia de tipo. `duplicate` lo acepta opcional: omitido,
  la copia conserva el tipo, y con `transport-document` sobre una comercial crea
  una plantilla de control nueva que solo copia diseño, letra, colores y marca.
  De control a comercial, o un valor desconocido, es `422`
  `invalid_pdf_template_configuration` con `param` `document_kind`, y sin el
  módulo de documentos de transporte es `403` `feature_not_available_in_plan`.
  `pdf-templates defaults-set` solo admite plantillas
  de control (`transport-document`) en el valor por defecto de los documentos de
  transporte. Trece operaciones declaran ahora `503` reintentable
  (`pdf_engine_unreachable`, con `Retry-After` y sin nada escrito) cuando el
  motor de PDF no responde: `invoices issue`, `invoices send`,
  `quotes send`, `proformas send`, `delivery-notes send`, `quotes accept`,
  `proformas accept`, `invoices substitute-simplified`, `revise-presentation`
  de facturas, presupuestos, proformas y albaranes y la vista previa del
  borrador de factura. Las doce primeras lo necesitan solo para comprobar la
  presentación revisada o construir la revisión. Los fallos terminales del
  motor siguen siendo el `500` `pdf_engine_profile_unavailable`. Las
  descripciones de `tax_rate` y `retention_rate` de las líneas explican la
  elección por `tax_rate_id` o `retention_rate_id` y el `422`
  `parameter_invalid_value` con subcódigo `tax_rate_mismatch` cuando no
  coinciden.
- **FACe sin reintento automático.** `invoices face-submissions submit` y
  `face-submissions cancel` hacen una sola llamada: una transmisión incierta no
  se repite a ciegas. Consulta `invoices face-submissions list` antes de volver
  a enviar.
- Un campo que el contrato reserva (esquema `false`) ya no genera flag, y los
  cuerpos que referencian un esquema con ejemplos ganan sus flags tipados.

## [0.9.0] - 2026-10-17

El artefacto de este repositorio YA viaja con el contrato del eje: `internal/spec/openapi.json` embebe el documento de **664 operaciones** en **534 paths**, el mismo que sirve producción desde el despliegue del 2026-10-17, y `internal/cmd/resources_gen.go` se ha regenerado entero desde él.

### Cambiado — rotura

- **Todo comando de un recurso de empresa gana el segmento de empresa.** En la
  v1, los recursos de una empresa cuelgan de `/v1/companies/{company}/…`: la
  empresa deja de deducirse en silencio de la credencial y pasa a viajar en la
  invocación, donde se ve, se registra y se puede auditar.

  ```console
  # Antes
  $ factuarea invoices list --json

  # Ahora
  $ factuarea invoices list acme_co --json
  ```

  Alcance: de las **508** operaciones del contrato, **432** cuelgan del eje de
  empresa, **60** del eje de cuenta (`/v1/accounts/{account}/…`, la cuenta
  propia), **6** son de cuentas gestionadas (la cuenta es el recurso), **5** son
  rutas raíz de provisión e importación y **5** son catálogos e identidad de
  raíz (432 + 60 + 6 + 5 + 5 = 508), así que la rotura toca casi todo el árbol. **Ningún comando se renombra**: los que dejan de existir se RETIRAN
  (las bajas se listan más abajo, cada una con su sustituto cuando lo hay) y
  los demás conservan su nombre; lo que cambia es cómo se invoca. Lo que se escribe en ese segmento es el `id` de la empresa —el que
  publica `factuarea account show` en `data.scope[].id`—, nunca su NIF.

  Ojo con el marcador: en las **8** operaciones de
  `/v1/accounts/{account}/companies/{company}` —los NIF de la cuenta—
  `{company}` es el **recurso** y no el eje, así que ahí se escribe siempre. La
  CLI las distingue por el path, nunca por el nombre del parámetro.

  Una sola ventana de rotura: ver https://docs.factuarea.com/docs/changelog/axis-and-payload-breaking-window

  Esa entrada anuncia la rotura **entera** de la v1 —el re-anclaje de los
  recursos al eje de empresa y el cambio de payload que viaja con él: la fecha
  de cobro renombrada (`paid_at` → `paid_on`) y los importes, que pasan de
  número a cadena— como **una sola rotura en una sola ventana**. No son dos
  anuncios ni dos migraciones: quien adapte su integración una vez, la adapta
  entera. La entrada la publica el portal de documentación, que va después de
  este trabajo; esta nota solo la referencia y no la reproduce.

- **El modelo de cuentas: el vale de la cuenta se retira y lo sustituye el
  traspaso de NIF.** Las tres operaciones de `account claim-tokens` desaparecen
  del árbol y cada una tiene su pareja:

  | Antes (retirada) | Ahora |
  | --- | --- |
  | `factuarea account claim-tokens accept` | `factuarea account nif-transfers accept` |
  | `factuarea account claim-tokens create` | `factuarea account nif-transfers create` |
  | `factuarea account claim-tokens list`   | `factuarea account nif-transfers list` |

  `create` y `accept` son irreversibles y nombran la cuenta como último
  argumento: se escribe como argumento posicional, `--account` no la rellena. Se
  retiran además, sin pareja, las seis operaciones de impuestos de empresa
  `taxes create`, `taxes update`, `taxes delete`, `taxes set-default`,
  `taxes set-default-for-document` y `taxes toggle`. En total, **9 bajas** (las 3 de `claim-tokens`, que tienen
  pareja, y las 6 de impuestos, que no) y **36 altas**; son retiradas y altas
  del árbol, no renombres: el árbol pasa de 481 a **508** comandos (**432** del eje de empresa,
  **60** del de cuenta, **6** de cuentas gestionadas, **5** de provisión raíz y
  **5** de catálogos e identidad).

  Una sola ventana de rotura: ver https://docs.factuarea.com/docs/changelog/axis-and-payload-breaking-window

  Novedades del modelo de cuentas: https://docs.factuarea.com/docs/changelog/account-model

### Añadido

- **Tres formas de decir la empresa, y una de no decirla**, para que la
  mitigación esté en el mismo sitio que la rotura:

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

- **Resolución automática cuando la credencial alcanza un solo NIF**, tercer
  eslabón de una cadena de cuatro: argumento posicional → flag persistente
  `--company` → el ámbito de la credencial (`GET /v1/me`, campo `data.scope`)
  cuando alcanza **una sola** empresa → error accionable. El valor resuelto se
  recuerda durante esa invocación y **jamás se escribe en disco**: un valor
  persistido sobreviviría a la rotación de la clave o al cambio de perfil, y
  acabarías operando en silencio sobre una empresa fuera de tu ámbito. No
  existe `FACTUAREA_COMPANY` ni clave de empresa en `config.toml`, y su
  ausencia es una decisión, no un olvido.

- **Errores de uso accionables**, por stderr, con código de salida `2` y antes
  de ejecutar la operación. Cuando la credencial alcanza varias empresas, el
  mensaje enumera el ámbito —`id` a la izquierda, que es lo que la flag come;
  NIF y nombre a la derecha, que es lo que tú reconoces— para que la orden
  siguiente se copie y se pegue:

  ```console
  $ factuarea invoices show inv_01931b3e
  Error: tu credencial alcanza 2 empresas y no elijo por ti; pasa una como argumento posicional (<company>) o fíjala con --company:
    --company cmp_01931b3e7a2c   B12345678 — Acme Sillas SL
    --company cmp_01934c8d2e5f   B87654321 — Nordic Muebles SL
  ```

  Aportar la empresa por las dos vías a la vez también es error de uso: la CLI
  no elige por ti. Y en las operaciones irreversibles que actúan sobre la
  empresa **entera**, `--company` no la rellena a propósito, porque confirmar
  sobre un identificador que no has escrito no confirma nada.

- **El hueco de la empresa se publica aparte para los agentes**:
  `factuarea commands --json` lo declara en `optional_args`
  (`{"company": "--company"}`), sin mover el orden ni los nombres de `args`.

- **Superficie nueva del contrato del eje: 17 operaciones** respecto al contrato
  de 469 (15 del eje de cuenta —`members`, `invitations`, `nif-transfers`,
  `owner transfer`, `usage`, `module-access`—, más
  `companies issuing-readiness` y `recurring-invoices bulk-status`), que con 5
  bajas dejó 481. Encima van las **36 altas** del modelo de cuentas (cuentas
  gestionadas, webhooks y request logs de cuenta, entre otras; 481
  + 36 − 9 = 508). Es superficie **añadida**: no consume ventana de rotura, y
  ya está en el árbol de comandos.

- **Las cinco operaciones de API keys se mudan al eje de cuenta.** Desaparecen
  `factuarea companies api-keys {list,create,show,revoke,rotate-secret}` y las
  publica `factuarea account api-keys …` sobre `/v1/accounts/{account}/api-keys`:
  una clave alcanza una cartera, no una empresa suelta.

- **El devloop y el arranque de sesión se re-anclan al contrato.** `listen`,
  `trigger` y la comprobación de `login` consultaban rutas planas que la v1 ya no
  declara: la identidad de la credencial pasa de `/v1/account` a `/v1/me` (y se
  lee del propio contrato embebido, no de un literal), y el feed de eventos y los
  fixtures de `trigger` cuelgan ya de `/v1/companies/{company}/…`, resolviendo la
  empresa por la MISMA cadena de precedencia que el árbol generado.

- **El eje de cuenta tiene su propia flag, `--account`**, persistente como
  `--company`. Rellena el `{account}` de `/v1/accounts/{account}/…` y hace opcional
  su posicional: `factuarea account members list --account <account>`. Precedencia:
  posicional → `--account` → solo en las lecturas, la cuenta propia de tu credencial
  (`GET /v1/me`, `data.account.id`; se recuerda por invocación y jamás se escribe
  en disco). **Las escrituras no deducen la cuenta**: sin posicional ni `--account`
  terminan con error de uso (código `2`) sin enviar nada, y el mensaje nombra la
  cuenta de tu credencial. En las irreversibles cuyo último parámetro es la propia
  cuenta (`account nif-transfers create|accept`, `account owner transfer`) ni la
  flag vale. En `managed-accounts` la cuenta es el recurso y se escribe siempre;
  las rutas raíz de provisión e importación no llevan cuenta, y `--company` nunca
  rellena la cuenta. `factuarea commands --json` lo declara en `optional_args`
  (`{"account": "--account"}`).

- **Grupos nuevos**: `managed-accounts` (`list`, `create`, `show`, `access-level`,
  `plan`, `management end`, `claim-tokens create` —el reemisor del vale de la
  cuenta GESTIONADA, no el vale retirado de la cuenta propia— y `imports
  {create,preview,template}`, con ficheros por `--file-accounts_file` y
  `--file-customers_file` y las opciones por `-d`), `account webhooks` (con
  `deliveries`), `account request-logs`, `account nif-transfers` y la hoja
  `account provisioning-usage`.

[Unreleased]: https://github.com/factuarea/factuarea-cli/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/factuarea/factuarea-cli/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/factuarea/factuarea-cli/compare/v0.8.0...v0.9.0
