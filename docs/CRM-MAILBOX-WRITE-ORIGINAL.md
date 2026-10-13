# Recibos originales de buzones CRM

El CLI consume dos GET v1 nativos de `conversations.configure_mailbox`: `getCreatedCrmMailboxWriteOriginal` y `getUpdatedCrmMailboxWriteOriginal`. La credencial API key existente fija empresa y entorno. Ambos requieren `crm:write`; `conversations:read` y `crm:read` no sustituyen ese permiso. El servidor conserva C7, ACL, máscaras y autoridad actuales. Esta fuente no activa capacidades ni concede consentimiento OAuth.

```sh
factuarea crm mailboxes recover-create --idempotency-key-file original-key.txt --original-wire-file original-create.json --json
factuarea crm mailboxes recover-update <mailbox-UUIDv7> --idempotency-key-file original-key.txt --original-wire-file original-update.json --json
```

`--original-wire` acepta JSON literal o `-` para stdin. Usa exactamente los bytes del cuerpo enviado originalmente: espacios, orden, escapes, Unicode y CAS forman parte del fingerprint. El CLI los codifica una sola vez en el único parámetro `original_wire`, sin cuerpo GET ni parámetros de empresa. La clave se conserva sin trim ni normalización: 1–512 bytes, sin espacios ASCII ni controles. Un fichero de clave con salto final se rechaza. Nunca se genera una clave nueva.

El JSON original contiene exactamente `expected_version`, `provider`, `connection_id`, `kind`, `team_id` y `allowed_user_ids`. El alta exige versión 0; la actualización exige CAS positivo y `connection_id` UUIDv7. `provider` es `gmail` o `microsoft_graph`; `kind` es `personal` o `shared`, con `team_id: null` obligatorio para personal. La lista de usuarios conserva el contrato UUID histórico del dueño, admite hasta 1000 identidades y rechaza duplicados incluso si cambia el uso de mayúsculas.

La salida conserva el envelope nativo `data` con `id`, `effect_id`, `connection_id`, `version`, `availability`, `confirmed_at` y `provider`. Se valida destino, conexión, proveedor y `expected_version + 1` sin redondear enteros. Una actualización posterior no sustituye el primer recibo por el head actual. `disconnected` no acredita acceso al proveedor; un recibo incompatible no confirma la escritura.

Las consultas realizan un único intento y no siguen redirects, repiten comandos ni buscan un head alternativo. Se mantienen status, code, subcode, details, request_id y los exit codes existentes: autenticación 3, permisos 4, validación 5, ausencia 6, admisión 7, conflicto 8, servidor 9 y red 10. Una ausencia 404, un conflicto 409, un efecto pendiente/no confirmado o un fallo de infraestructura se propaga sin crear otra identidad ni convertirlo en éxito. El store propietario actual puede propagar una incertidumbre como 500; el CLI conserva ese error aunque el OpenAPI enumere el caso pendiente en 409. Un fallo de transporte no expone la query privada en stderr.

Este incremento concreta CRM-082 / `crm-cli-delivery` §2.1–2.4 para estos dos métodos nativos ausentes. No completa el inventario CRM, la entrega global, una publicación o una prueba de API/SQL: sus pruebas HTTP son offline con credenciales sintéticas.
