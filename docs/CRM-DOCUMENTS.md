# Documentos CRM v1 — entrega nativa privada

Este grupo consume las 14 APIs v1 JSON de CommercialDocument y OpportunityDocument. La autenticación sigue usando `FACTUAREA_API_KEY`, el almacén nativo y `--profile` / `FACTUAREA_PROFILE`. El prefijo de la credencial selecciona sandbox o live; las mutaciones live requieren `--live`. Company, actor, scopes y disponibilidad efectiva se resuelven en el servidor. `--active-profile` transmite el `X-Active-Profile` gestionado autorizado y `--api-version` conserva `Factuarea-Version`.

| Comando después de `factuarea crm` | Método y ruta v1 | Scope |
| --- | --- | --- |
| `documents list` | GET `/v1/crm/documents` | `crm_documents:read` |
| `documents get <document>` | GET `/v1/crm/documents/{document}` | `crm_documents:read` |
| `documents upload` | POST `/v1/crm/documents` | `crm_documents:write` |
| `documents replace-version <document>` | POST `/v1/crm/documents/{document}/versions` | `crm_documents:write` |
| `documents share <document>` | POST `/v1/crm/documents/{document}/shares` | `crm_documents:write` |
| `documents revoke-share <document>` | POST `/v1/crm/documents/{document}/shares/revoke` | `crm_documents:write` |
| `documents archive <document>` | POST `/v1/crm/documents/{document}/archive` | `crm_documents:write` |
| `documents download <document>` | GET `/v1/crm/documents/{document}/download` | `crm_documents:read` |
| `opportunity-documents list <opportunity>` | GET `/v1/crm/opportunitydocuments/{opportunity}/documents` | `crm_opportunitydocuments:read` |
| `opportunity-documents candidates <opportunity>` | GET `/v1/crm/opportunities/{opportunity}/documents/candidates` | `crm_opportunitydocuments:read` |
| `opportunity-documents create-quote <opportunity>` | POST `/v1/crm/opportunitydocuments/{opportunity}/quotes` | `crm_opportunitydocuments:write` |
| `opportunity-documents link <opportunity>` | POST `/v1/crm/opportunitydocuments/{opportunity}/documents` | `crm_opportunitydocuments:write` |
| `opportunity-documents unlink <opportunity> <link>` | DELETE `/v1/crm/opportunitydocuments/{opportunity}/documents/{link}` | `crm_opportunitydocuments:write` |
| `opportunity-documents financial-summary <opportunity>` | GET `/v1/crm/opportunitydocuments/{opportunity}/financial-summary` | `crm_opportunitydocuments:read` |

Upload, create-quote y link comprueban `201`; las demás mutaciones comprueban `200`. La diferencia frente al snapshot inicial del SDK PHP para create-quote/link se resolvió contra los controllers v1 existentes. Los comandos `documents recover` y `opportunity-documents recover` son replay explícito de la operación original; no anuncian una API de recibos adicional.

Las identidades CRM y producer_reference_id son UUIDv7. `document_id` de Quote/Invoice/Contract/Recurring conserva UUID históricos del propietario, incluida su grafía original en el body. Los tipos actuales son `quote`, `invoice`, `contract`, `recurring_schedule`. La revisión de un candidato es informativa: link recibe solamente document_type, document_id e is_primary, sin CAS de propietario inventado. `is_primary:false` acepta el replay nativo de una asociación ya primaria.

## Lecturas y paginación

```sh
factuarea --profile sandbox crm documents list --page 1 --per-page 25 --archived=false --json
factuarea --profile sandbox crm opportunity-documents list "$OPPORTUNITY_ID" \
  --status linked --is-primary=false --all --limit 25 --max-pages 100 --json
factuarea --profile sandbox crm opportunity-documents candidates "$OPPORTUNITY_ID" \
  --document-type recurring_schedule --search '' --json
factuarea --profile sandbox crm opportunity-documents financial-summary "$OPPORTUNITY_ID" --json
factuarea --profile sandbox crm documents download "$DOCUMENT_ID" \
  --document-version 2 --output ./document-v2.pdf --json
```

Library usa page/per_page; asociaciones y candidatos usan cursor opaco y limit. `--all` emite NDJSON con límite explícito, conserva los cursores sin reinterpretarlos y detecta ciclos. Las páginas no constituyen un snapshot transaccional y un fallo posterior puede dejar salida parcial. `candidates` conserva available/unavailable_reason del productor, también con `--all`. El listado de asociaciones admite los flags document-type, status, is-primary, sort, direction o el objeto cerrado `--filters`; search pertenece a biblioteca y candidatos.

JSON conserva el envelope nativo, los campos enmascarados omitidos y decimal strings exactas. La tabla mantiene los UUID e importes por divisa y elimina controles de terminal de textos remotos. No hay suma FX ni importes fiscales para Contract/Recurring. Download guarda los bytes autenticados en un fichero nuevo 0600 y emite metadata JSON; rechaza redirects, respuestas parciales y sobrescrituras.

## Mutación y recuperación del intent original

Cada mutación requiere una clave ASCII original de 1–255 caracteres y un `--intent-file` nuevo. Los espacios exteriores de una clave se rechazan porque HTTP los eliminaría. `--skeleton` y `--dry-run` funcionan sin autenticación ni envío. Los cuerpos son objetos JSON cerrados; las claves repetidas, coerciones numéricas, autoridad en body y CAS ajenos al contrato se rechazan. Omitir un opcional lo conserva; null sigue siendo explícito.

```sh
factuarea crm documents upload --skeleton
factuarea --profile sandbox crm documents upload \
  --data-file ./upload.json --idempotency-key "$ORIGINAL_KEY" \
  --intent-file ./upload-intent.json --json

# Solo ante resultado sin confirmar, bajo el mismo servidor/credencial/perfil:
factuarea --profile sandbox crm documents recover \
  --intent-file ./upload-intent.json --json

factuarea crm opportunity-documents create-quote "$OPPORTUNITY_ID" --skeleton
factuarea --profile sandbox crm opportunity-documents link "$OPPORTUNITY_ID" \
  --data-file ./link.json --idempotency-key "$LINK_ORIGINAL_KEY" \
  --intent-file ./link-intent.json --json
```

Upload/replace-version registran una referencia de ingestión acreditada: title (upload), producer_reference_id y claimed_mime, más expected_version (replace). No se envían archivos multipart SPA ni URLs públicas. Share admite grant_type user/team, grantee_id, expected_version y expires_at omitido/null/timestamp con zona. Archive, revoke-share y unlink exigen además la confirmación nativa `--confirm=<UUID exacto>` en ejecución no interactiva, tanto en el primer envío como en recover.

Create-quote exige draft con date y recipient (unassigned/commercial_counterparty/business_contact), con opcionales nativos expiry_date, series_id, client_id, notes, terms y expected_root_version/expected_line_version. contact_id puede omitirse o ser null/UUIDv7. No añade líneas, totales, permisos o promoción fiscal.

El checkpoint contiene método, ruta, bytes JSON originales, key, servidor, perfil, entorno y headers de versión/perfil, con huella de credencial y sello HMAC; no contiene la credencial. Se crea 0600 con exclusión de sobrescritura y se sincroniza antes del envío. Es un intent local, no evidencia de efecto confirmado. Recover verifica sello, propietario, binding y contrato; reutiliza el mismo checkpoint sin actualizar CAS, consultar la última revisión, cambiar key ni reescribirlo. Rotar la credencial, cambiar servidor/perfil/entorno o alterar el intent bloquea el dispatch.

El transporte hace un único envío, sin retry automático, redirect ni replay de body por HTTP. Timeout, transferencia truncada, 5xx, redirect, recibo mal formado y 403/404 posteriores al envío dejan el intent sin confirmar. JSON de error conserva type/code/message/request_id y añade unconfirmed, intent_file y recovery_command cuando corresponda. 409 CAS/idempotencia, 422 validación y 429 cuota conservan los errores y exit codes nativos. Ningún error se convierte en recibo sintético.

## Mapeo de tareas originales y límites

CRM-082 / CRM-144, `crm-cli-delivery`:

| Tarea original | Ficheros y símbolos reales de este corte |
| --- | --- |
| 1.1 reconocimiento / 1.3 integración nativa | Go 1.26.4, Cobra; root.go/commands.go; newCRMDocumentContext reutiliza ResolveAPIKey y guard live del host |
| 1.2 referencia BottleCRM / CRM-249, CRM-253 | native-integration, documentation_reference; sin código upstream copiado ni infraestructura ORM/auth importada |
| 2.1 inventario | crmDocumentOperations / crmDocumentManifest: 14 APIs y dos recovery locales |
| 2.2 comandos y errores | registerCRMDocumentCommands, newCRMDocumentCommand, dispatchCRMDocumentWrite; contratos v1 del handoff PHP y controllers nativos |
| 2.3 salida / páginas | printCRMDocumentBody, runCRMDocumentPages; máscaras omitidas, cursor y Money por divisa |
| 2.4 ayuda / completion | Cobra y completion local de tipos, sin descargar carteras |
| 2.5 pruebas afectadas | crm_documents_test.go: rutas, sandbox/live, scopes, recovery, CAS, errores, máscaras, decimal strings, límites |
| 2.6 receta | Ejemplos anteriores del grupo de documentos; el recorrido completo Lead→Opportunity→Activity no pertenece a este corte |
| 2.7 / 3.1 entrega externa | Diff privado del repo CLI con hashes del handoff; sin generated/spec, package bump ni publicación |

La disponibilidad CRM y SourceDelivery permanecen OFF / no ratificadas. Estas pruebas ejercitan el CLI con contratos HTTP locales; el runtime SQL/backend, la instalación comercial y la release necesitan evidencias propias. La arquitectura BottleCRM fijada en 656aaa6c3362658b82a3a6043c9e97a88bb8b215 es referencia de documentación: este código implementa el contrato nativo Factuarea, sin traducción sustancial de código upstream. No modifica el SDK Node ni los bloques app TaskAgenda/iCal congelados.
