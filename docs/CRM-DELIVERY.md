# Entrega incremental CRM del CLI — DEV-2

Esta entrega implementa CRM-082 / CRM-144 para las operaciones v1 actuales de ContactPeople, Lead y Pipeline. El reconocimiento del repositorio reconcilia los targets TypeScript propuestos por `crm-cli-delivery` con el CLI real Go/Cobra. La base es `3ca2754`, en la rama local `feature/dev-2-crm-cli-delivery-20261009`; no había WIP al abrirla.

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

El inventario de este grupo incluye ContactPeople11, Lead23 y Pipeline15, 49 operaciones nativas en total. Opportunity y Activity no están implementados por esta entrega; no se publica una receta Lead→Opportunity→Activity ficticia ni se declara CRM-082 completo. Tampoco se incorpora el catálogo posterior de otros BCs. La capacidad actual o ausente sigue siendo una decisión del backend; estas fuentes no cambian flags, tiers, cuotas o activación.

La referencia BottleCRM `docs/architecture/overview.md` es comparativa: no tiene un CLI Go equivalente que portar y no se copia código upstream. Esta implementación integra los productores nativos de Factuarea, su cliente existente y sus contratos; CRM-249 / CRM-252 se trazan mediante el inventario, hashes, fixtures HTTP y resultados del handoff privado. Los tests del CLI usan servidores locales aislados y credenciales sintéticas, no pruebas SQL ni un sandbox remoto acreditado. La instalación de un binario construido y las comprobaciones dirigidas no equivalen a publicación, rollout o aceptación comercial.

La evidencia ejecutada, hashes de fuentes y resultados de compatibilidad están en el handoff privado del bloque. Commit, push, publicación y la receta completa de los BCs posteriores pertenecen al cierre autorizado por Root.
