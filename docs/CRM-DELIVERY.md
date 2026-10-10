# Entrega incremental CRM del CLI — DEV-2

Esta entrega implementa CRM-082 / CRM-144 para las operaciones v1 actuales de ContactPeople, Lead, Pipeline, Knowledge Articles y Public Help Center. El CLI real es Go/Cobra. El incremento KB23/PCH5 parte de `d0ebdf2bd9fa908aa8e3bd41b186a8fb2383e6ad`, en `feature/dev-2-crm-cli-delivery-20261009`, y consume el contrato nativo congelado de 77 operaciones / 66 paths / 106 schemas, SHA256 `4a2cb151faef526aa8c8fd26c29a1d3f6acd019335e13b058b86671f40eb609f`. La entrega es fuente con activación OFF.

Los comandos se generan mediante `internal/gen` desde `internal/spec/openapi.json`. Los identificadores de operación del backend se conservan literalmente. El adaptador reconoce `x-crm-operation` y el scope propio; esos metadatos sirven para generar el árbol, nunca conceden permiso. `docs/crm-command-contract.json` registra el inventario y la procedencia exactos del grupo. Las operaciones anteriores permanecen en el snapshot.

## Comandos y autenticación

Utiliza la API key mediante `factuarea login`, el perfil existente o `FACTUAREA_API_KEY`, sin incluirla en argumentos, archivos de ejemplo o logs. La credencial fija la empresa y el entorno; no se crea un selector de empresa ni un grant local. Las operaciones con una key LIVE conservan `--live` cuando corresponda.

```sh
factuarea commands --json
factuarea crm contact-people list --kind person --page 1 --per_page 25 --json
factuarea crm pipelines list --status active --sort name_asc --json
factuarea crm leads list --filters '{"status":"open","score_min":0}' --json
factuarea crm leads list --paginate --max-pages 20 --json
factuarea crm leads create --data-file lead.json --idempotency-key lead-original-001 --json
factuarea crm leads show <id-UUIDv7> --json
factuarea crm leads update <id-UUIDv7> --data-file reviewed-lead.json --idempotency-key lead-original-002 --json
```

`lead.json` usa el contrato cerrado actual, por ejemplo `{"name":"Interés revisado","source":"manual"}`. Una edición envía la `expected_version` observada junto a sus cambios. Los campos omitidos se conservan; `null` sólo limpia los opcionales admitidos. El CLI no genera UUIDs de entidades: conserva los `id` y `*_id` públicos del servidor. Las referencias con un contrato propio, como usuarios anteriores, no se convierten por suposición en UUIDv7 nuevos.

Las ayudas, `--skeleton` cuando exista un schema de campos y `--dry-run` permiten revisar el cuerpo sin llamar a la API. Una acción cuyo metadata nativo exige confirmación usa el mecanismo existente `--confirm` o el prompt interactivo; `--no-input` nunca confirma automáticamente. Los previews y sus planes pertenecen al productor; no se fabrica un token de remapping o conversión.

## Knowledge Articles y Public Help Center

El incremento añade 23 operaciones Knowledge Articles y cinco Public Help Center. `commands --json` publica los identificadores originales, los dos scopes requeridos, schemas de petición/respuesta/consulta, límites de cuerpo y flags de recibo. El scope fino siempre se exige junto a `customer_service:read` o `customer_service:write`; incluso las lecturas administrativas y de recibos de Public Help Center requieren ambos scopes de escritura. El servidor conserva todos sus guards actuales.

```sh
factuarea crm knowledge-articles search --query CRM --locale es --page 1 --per_page 25 --json
factuarea crm knowledge-articles show <article-UUIDv7> --json
factuarea crm knowledge-articles versions <article-UUIDv7> --json
factuarea crm knowledge-categories list --json
factuarea crm knowledge-articles suggest <ticket-UUIDv7> --ticket_version <CAS-observado> --audience internal --json
factuarea crm knowledge-articles create --data-file article.json --idempotency-key kb-create-original-001 --confirm create --json
factuarea crm knowledge-articles save <article-UUIDv7> --data-file article-save.json --idempotency-key kb-save-original-001 --confirm <article-UUIDv7> --json
factuarea crm knowledge-articles receipt-save --idempotency-key kb-save-original-001 --json
factuarea crm knowledge-categories save --data-file category.json --idempotency-key category-original-001 --confirm save --json
factuarea crm knowledge-categories receipt --idempotency-key category-original-001 --json
factuarea crm public-help-center show --json
factuarea crm public-help-center publish --data-file center.json --idempotency-key center-original-001 --confirm publish --json
factuarea crm public-help-center publish-receipt --idempotency-key center-original-001 --json
factuarea crm public-help-center unpublish <center-UUIDv7> --data-file center-unpublish.json --idempotency-key center-original-002 --confirm <center-UUIDv7> --json
factuarea crm public-help-center unpublish-receipt --idempotency-key center-original-002 --json
```

Crear un artículo exige `expected_version: 0`, `title`, `body`, `editorial_locale`, `category_ids` y `slug`; `category_ids: []` es válido. `save` exige el cuerpo editorial completo y el CAS positivo observado. `submit`, `approve`, `publish`, `unpublish`, `archive` y `link` conservan sus cuerpos propios. Publicar un artículo exige también `published_version` y `audience`; vincularlo exige `target_type`, `target_id` y `target_version`. Estas escrituras exigen confirmación humana mediante el mecanismo existente, aunque el JSON del backend no incluya un campo `confirmed`.

Crear una categoría omite `id` y envía `expected_version: null`; editarla conserva su UUID y CAS positivo. Ambos casos envían `taxonomy_id`, `expected_taxonomy_version`, `parent_id` (incluido `null` para raíz), `name`, `slug`, `visibility` y `status`. Usa JSON para los `null` explícitos. Crear un help center omite `id` o lo envía `null`, con `expected_version: 0`; editarlo conserva el UUID y CAS positivo. Su publicación exige `confirmed: true`, `slug`, `display_name`, `locale` y `article_slugs`, que puede ser `[]`. Retirarlo exige `confirmed: true` y su CAS positivo; esos literales no sustituyen la confirmación humana de la CLI.

El CLI valida los cuerpos cerrados y consultas contra los schemas congelados, conserva los enteros CAS sin redondeo y mantiene los bytes de `-d`, stdin o `--data-file`. Los límites nativos son 450000 bytes para intenciones editoriales, 8192 para categorías y 180000 para Public Help Center. Los GET de recibo exigen una clave original explícita; no aceptan cuerpos ni generan claves. Un recibo inválido o ajeno al UUID/CAS de la intención no confirma una escritura. La recuperación mantiene `effect_id`, `operation`, confirmación y snapshot/CAS originales con las máscaras vigentes, y nunca hace un segundo envío.

La búsqueda KB ofrece `page` / `per_page`, pero su respuesta no declara continuación completa: no se ofrece `--paginate`. Tampoco se inventa paginación para categorías, versiones, sugerencias o recibos.

## Lecturas, máscaras y paginación

El servidor revalida scope, membresía, disponibilidad, campo, registro y C7 en cada llamada. La consulta local de scopes no sustituye ese control; `--skip-scope-check` tampoco elimina los guards del backend. Una revocación detiene el recorrido con el error original y no publica la siguiente página.

La salida normal conserva el envelope original de la API. `--paginate` emite las filas autorizadas como NDJSON cuando el schema declara una continuación completa:

- ContactPeople usa `page` / `per_page` y `meta.current_page` / `meta.last_page`; no se inventa un cursor. Las operaciones que sólo publican la página actual siguen ofreciendo `--page`, sin prometer un recorrido completo.
- Lead usa `cursor` opaco y `data.data`, `data.has_more` y `data.next_cursor`. El detalle paginado de un score run conserva `data.results` y su identidad `lead_id`.
- Pipeline usa `cursor` opaco y `data.items`, `data.has_more` y `data.next_cursor`.

Los filtros permanecen sin alterarse entre páginas. El presupuesto `--max-pages` es local al CLI, no una cuota comercial ni un límite del backend; su valor predeterminado es 100 peticiones. Si se agota, la salida queda expresamente parcial y el proceso falla. Un cursor repetido o ausente, identidad repetida o envelope incompatible falla sin inventar deduplicación, totales ni campos ocultos. Las páginas numeradas no tienen un snapshot global inventado: cada respuesta mantiene la autoridad y los datos actuales de su productor.

## Escrituras e identidad original

Cada escritura conserva una sola clave `Idempotency-Key` y el cuerpo original de la intención. Usa una clave explícita para poder reconciliar el paso después de terminar el proceso. La ausencia de una clave utiliza el generador canónico de claves de transporte, nunca un UUID de entidad.

Las escrituras CRM se envían en un solo intento: se desactiva tanto el retry explícito del cliente como el replay implícito del transporte HTTP en conexiones reutilizadas. Un timeout o pérdida del acuse produce `cli_mutation_unconfirmed`, incluye la clave original, termina con el código de red y no crea otra identidad ni reenvía automáticamente. Consulta y revisa antes de repetir explícitamente la intención original; no se declara éxito por una lectura distinta. El significado de un replay confirmado pertenece al ledger del dueño: no se sustituye el resultado por un recibo o versión fabricados en el CLI.

Los números JSON se preservan sin convertir el CAS a `float64`, incluso en detalles de error. Se conservan las cadenas decimales del contrato; no existe conversión FX. `403`, invisibilidad uniforme `404`, CAS / idempotencia `409`, campos `422`, admisión `429` y fallos del servidor mantienen códigos, detalles y los exit codes canónicos del CLI. Los cuerpos y credenciales no se añaden a mensajes locales de paginación.

## Alcance y evidencia

El inventario incluye ContactPeople11, Lead23, Pipeline15, Knowledge Articles23 y Public Help Center5: 77 operaciones nativas. Los 440 paths y todos los componentes anteriores se conservan; se añaden 26 paths, 33 schemas alcanzables y seis aliases de respuestas para conservar el Error nativo sin modificar el anterior. Opportunity y Activity no están implementados por esta entrega; no se declara CRM-082 completo. La capacidad actual o ausente sigue siendo una decisión del backend; estas fuentes no cambian flags, tiers, cuotas o activación.

La referencia BottleCRM `docs/architecture/overview.md` es comparativa: no tiene un CLI Go equivalente que portar y no se copia código upstream. Esta implementación integra los productores nativos de Factuarea, su cliente existente y sus contratos; CRM-249 / CRM-252 se trazan mediante el inventario, hashes, fixtures HTTP y resultados del handoff privado. Los tests del CLI usan servidores locales aislados y credenciales sintéticas, no pruebas SQL ni un sandbox remoto acreditado. La instalación de un binario construido y las comprobaciones dirigidas no equivalen a publicación, rollout o aceptación comercial.

La evidencia ejecutada, hashes de fuentes y resultados de compatibilidad están en el handoff privado del bloque. Commit, push, publicación y la receta completa de los BCs posteriores pertenecen al cierre autorizado por Root.
