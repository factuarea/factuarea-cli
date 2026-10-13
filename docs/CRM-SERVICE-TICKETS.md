# Tickets CRM: comandos nativos v1

Este bloque añade `factuarea crm service-tickets` al checkout nativo editable. Reutiliza la resolución existente de autenticación, entorno y perfiles: `FACTUAREA_API_KEY`, almacén de credenciales y `--profile`. Company, actor y sandbox proceden de la credencial y del contexto autorizado en el servidor. `--active-profile` conserva el header nativo `X-Active-Profile`; `--api-version` conserva `Factuarea-Version`.

| Comando | Operación nativa | Método y ruta | Scope |
| --- | --- | --- | --- |
| `stage <ticket>` | `moveServiceTicketToStage` | POST `/v1/crm/service-tickets/{ticket}/move-to-stage` | `customer_service:write` |
| `attach <ticket>` | `attachServiceTicketDocument` | POST `/v1/crm/service-tickets/{ticket}/attachments` | `customer_service:write` |
| `detach <ticket>` | `detachServiceTicketDocument` | POST `/v1/crm/service-tickets/{ticket}/attachments/remove` | `customer_service:write` |
| `get <ticket>` | `getServiceTicket` | GET `/v1/crm/service-tickets/{ticket}` | `customer_service:read` |
| `list` | `listServiceTickets` | GET `/v1/crm/service-tickets` | `customer_service:read` |
| `timeline <ticket>` | `getServiceTicketTimeline` | GET `/v1/crm/service-tickets/{ticket}/timeline` | `customer_service:read` |
| `recover` | Recuperación local del intent | Repite el POST original de una de las tres mutaciones | `customer_service:write` |

Las seis respuestas nativas son HTTP 200 JSON. El manifiesto local `factuarea commands --json` y la ayuda declaran esos comandos, scopes y cuerpos. Las tres mutaciones tienen `requiresConfirmation=false` en el productor: detach retira una asociación existente y conserva el documento. El CLI exige `--live` con credenciales live, además de la autorización efectiva que revalida el servidor. El preflight `/v1/account` usa los mismos headers; si no está disponible, no acredita grants y la ruta de destino sigue comprobando la operación.

## Cuerpos originales y espera del cliente

Cada mutación requiere `--data` (JSON literal, `@fichero` o `-` para stdin) o `--data-file`, `--idempotency-key` original y `--intent-file` nuevo. UUIDv7 y enteros JSON positivos se validan antes de enviar. No se normalizan UUID, body, clave o CAS; no se convierten números mediante float. Los campos del body son cerrados:

- stage: `expected_version`, `pipeline_id`, `stage_id`; opcionales `waiting_reason` y `customer_message_id`.
- attach: `expected_version`, `document_id`, `document_version`. La revisión documental queda fijada a la versión declarada.
- detach: `expected_version`, `attachment_id`, obtenido de una lectura autorizada del ticket o su timeline.

En stage, omitir `waiting_reason` conserva la causa existente, `"waiting_customer"` abre la espera y `null` la libera. Un `customer_message_id` no nulo sólo se admite con `waiting_reason:null`; debe ser el UUID original de la respuesta factual del cliente. `customer_message_id:null` puede acompañar una causa omitida o declarada. El servidor verifica Company, sandbox, participante, ciclo y evidencia temporal; el CLI no fabrica comentarios, fechas o correlaciones. La etapa nativa determina el estado y el servidor conserva su validación de Pipeline/WIP.

```sh
factuarea crm service-tickets stage 01900000-0000-7000-8000-000000000001 \
  --data '{"expected_version":4,"pipeline_id":"01900000-0000-7000-8000-000000000002","stage_id":"01900000-0000-7000-8000-000000000003","waiting_reason":null,"customer_message_id":"01900000-0000-7000-8000-000000000004"}' \
  --idempotency-key 'ticket-stage-original-4' \
  --intent-file /ruta/privada/ticket-stage-original.json
```

`--skeleton` y `--dry-run` son locales y no requieren autenticación. `--dry-run` valida y muestra los bytes originales; no emite una mutación. El recibo confirmado contiene únicamente el UUID del ticket y su versión original resultante: CAS+1 en attach/detach; CAS o CAS+1 en stage (el productor permite mantener la misma etapa). Un UUID distinto o una revisión nueva ajena al intent quedan sin confirmar. El recibo no acredita un identificador de attachment, un envío público ni otros efectos ausentes del productor.

## Intent privado y recuperación explícita

Antes del único POST se crea `--intent-file` con permiso 0600, sin sobrescribir ficheros existentes. Guarda método, ruta, argumentos, bytes JSON, key, headers y binding al servidor, perfil, entorno y huella de la credencial. Un HMAC sella esos valores; la credencial no se copia al fichero. Recuperar exige el mismo contexto y una lectura del fichero regular privado original. Ficheros públicos, symlinks, cambios del body/CAS/key/headers o un intent de otro grupo se rechazan antes de despachar.

Una respuesta perdida, incompleta o inválida, HTTP 5xx/403/404, redirect o un código/subcódigo nativo `unconfirmed`, `revoked` o `authorization_changed` produce un resultado sin confirmar y conserva el intent. No hay retry automático ni redirects, ni endpoint ficticio de recibos. Los errores CAS 409, validación 422 y throttling 429 conservan su tipo y request ID. La recuperación vuelve a enviar exactamente el POST original; el productor nativo recupera su recibo idempotente sin duplicar efectos.

```sh
factuarea crm service-tickets recover --intent-file /ruta/privada/ticket-stage-original.json
```

Se conservan los `--profile` y `--live` originales cuando correspondan. recover sólo admite el fichero original y `--skip-scope-check`; no admite overrides de body, key, CAS o headers. El checkpoint no se reemplaza al recibir el recibo.

## Lecturas y paginación

get conserva la proyección actual, incluidos campos omitidos por máscaras. list y timeline usan `--page` (1–1000000) y `--per-page` (1–100, por defecto 50). list admite `--status` (`new`, `assigned`, `pending`, `closed`, `rejected`, `duplicate`), `--sort` (`id`, `version`, `title`, `status`, `priority`, `created_at`, `updated_at`) y `--direction` (`asc`, `desc`). No se inventan filtros ni inventario de etapas.

`--all` emite NDJSON por elemento y respeta `--max-pages` (1–10000). Un límite alcanzado o una página sin avance devuelve un error explícito con salida parcial. Cada página respeta la autoridad actual del servidor; la secuencia no es un snapshot. timeline conserva el UUID y la versión de cada snapshot histórico y no sustituye su contenido por el ticket actual. Las proyecciones de hijos son cerradas, sin rutas de storage ni URLs firmadas; la salida humana limpia caracteres de control.

## Correspondencia y límite de entrega

La correspondencia original es `crm-cli-delivery` (CRM-082): integración nativa de comandos/ayuda/manifiesto (1.1, 1.3), contrato, auth/contexto, errores, paginación y recuperación (2.1–2.7), y pruebas afectadas (3.1, parcial del conjunto del producto). El dominio productor pertenece a `crm-tenant-helpdesk` (CRM-168/CRM-170): cambios de etapa y asociaciones documentales reales; las reglas de espera factual complementan ese productor. La reutilización arquitectónica del brief de BottleCRM permanece como referencia, sin copiar código ni escribir el repositorio de referencia.

La base semántica es el handoff CustomerService PHP congelado con 71 pruebas y 350 aserciones. Sus DTO de stage aún no incluyen los dos campos opcionales de espera; este CLI sigue el Request/Controller v1 actual y el metadata Root de las tres operaciones. La proyección pública actual del Mapper no expone `waiting_facts`; el CLI no simula esa lectura. Consent, portal, creación de ticket, publicaciones, notificaciones, stage-list y efectos de entrega quedan fuera de estas seis rutas. Los flags de entrega/readiness permanecen OFF y la matriz sin ratificar; las pruebas locales de contrato no certifican el runtime sandbox del servidor.

Este bloque no cambia spec/generated, versión del paquete, releases ni los Sources congelados de Documentos, TaskAgenda o iCal. Sólo añade su registro a root/manifiesto y sus cinco ficheros específicos. La verificación del cierre usa tests Go de tickets y el test del manifiesto afectado, vet del paquete y build nativo; no repite la suite congelada de Documentos ni ejecuta app/SQL.
