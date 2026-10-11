# Lectores sobre el contrato nativo privado139

Estas hojas consumen el OpenAPI exportado por el backend, SHA256
`438b12b53be7ae7535cd6c85ba7dea06c76d84a33024db0e0232ac8ba279be63`.
El perfil sigue `unreleased`, `published=false`, `ratified=false`,
`delivery_enabled=false` y capabilities vacías. No se cambia el spec distribuido
ni se publica una release. Cada ejecución nueva requiere `--source-contracts`
explícito; esa opción no concede scopes ni activa CRM.

El extractor `tools/crm-source139/extract.py` recibe el manifiesto del export
nativo, comprueba el SHA y copia las operaciones exactas y el cierre de sus
referencias. El descriptor generado conserva también los contratos incompletos
de Apply/Current como límites, y la ausencia de una ruta pública de restore.
No es una reconstrucción manual de OpenAPI.

## Original de importación

```sh
factuarea crm imports apply-original RUN_UUID \
  --original-body-file first-apply.json \
  --idempotency-key 'first literal key' \
  --source-contracts --json
```

La clave y el run son los del primer intento; para `retry_failed`, se conserva el
run padre. El SHA incluye todos los bytes del fichero, incluidos espacios y el
salto final. También admite `--original-body -` para stdin. Sólo envía GET a la
ruta publicada con `original_body_sha256` y `Idempotency-Key`; nunca sube ese
cuerpo, ejecuta filas ni genera una clave. HTTP no representa espacios al
inicio/final del valor de un header: si la clave los contiene, el lector rechaza
el intento en lugar de consultar silenciosamente otra identidad.

Conserva `missing`, `in_progress`, `unconfirmed` y `confirmed` sin promoverlos.
Sólo `confirmed` incluye `original_response` con status202 y el string body
Original exacto. Se imprimen el envelope y la revisión de autorización actuales;
no se usa una respuesta almacenada localmente. El preflight usa el scope real
`crm_imports:write` aunque sea un lector. El servidor relee Company, actor,
sandbox, credencial, C7 y máscaras. No exige una confirmación de escritura para
este GET. Ante pérdida de respuesta puede consultarse el mismo Original por
GET; no se reenvía el POST ni se interpreta Current como recibo.

## Agenda de una página

```sh
factuarea agenda source-read --from 2026-10-01 --to 2026-10-31 \
  --sources tasks,holiday --assignee-id me --source-contracts --json
```

La ventana incluye ambos extremos y como máximo93 días. `sources` omitido
solicita todas las capas; `--sources=` solicita ninguna. El servidor omite capas
sin scope o módulo y la salida conserva `sources` realmente servidas. No se
fabrica un UUID para IDs fiscales o de festivos. `has_more` permanece false y
`next_cursor` null; no hay bucle de paginación ni acciones de calendario externo.
El preflight nativo requiere `tasks:read` y la autorización de cada capa sigue
en el servidor. El comando distribuido `agenda list` queda intacto.

Ambos lectores reutilizan auth/config/envelope/errores del CLI, con cero retries
y redirects. No guardan selección, cuerpos, claves o checkpoints de PII. Los
dos casos HTTP preparados no se han ejecutado.

## Límites de este corte

- El export139 no incluye `/crm/documents/{document}/restore`; el carrier130
  sigue pendiente de montaje/export nativo. No se registra un comando ficticio.
- POST Apply no publica requestBody y declara200 genérico. No se inventa un
  cuerpo tipado ni una respuesta202 desde otra superficie.
- GET Current refiere `CrmSpreadsheetImportResource` como string en este
  export. No se publica un DTO alternativo; su corrección corresponde al
  productor OpenAPI. Current y Original continúan siendo lecturas distintas.

Una exportación nativa futura es requisito para ampliar esas operaciones. El
archivo generado se regenera desde un manifiesto nativo, sin editar schemas a
mano, sin `make generate` contra producción y sin cambiar el gate de release.
