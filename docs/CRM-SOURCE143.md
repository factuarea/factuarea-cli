# Controles nativos privados143 de Apply y Restore

Este corte se genera mecánicamente desde el export nativo143 de Root, SHA256
`59b6cd2664aa8524e96a7600565b70b5eae5f6beaa7527913d703adf653a2fca`.
No consume el ABI descriptivo140 como spec. El selector propio143 reutiliza el
cierre de `$ref` del extractor139 y mantiene separado su JSON derivado. No se
cambia `internal/spec/openapi.json`, comandos generados o publicación/release.
El perfil continúa unreleased, ratified false, capabilities vacías y delivery OFF.

Los comandos requieren `--source-contracts` explícito. `--schema` muestra la
operación nativa/ref closure; `--dry-run` valida y muestra sólo la plantilla,
sin auth/HTTP, identidad, raw o clave. `--confirmed` es confirmación humana de
cada writer; también se conserva el campo `confirmed:true` obligatorio del
contrato. LIVE requiere además el guard `--live` existente del CLI.

## Apply

```sh
factuarea crm imports source-current "$RUN_UUID" --source-contracts --json
factuarea crm imports source-apply "$RUN_UUID" --source-contracts --confirmed \
  --data-file "$FIRST_APPLY_BODY" --idempotency-key-env FACTUAREA_APPLY_INTENT --json
factuarea crm imports source-original "$RUN_UUID" --source-contracts \
  --data-file "$FIRST_APPLY_BODY" --idempotency-key-env FACTUAREA_APPLY_INTENT --json
```

El cuerpo cerrado se valida desde el requestBody143: expected_version,
preview_hash, effect_uuid, mode y confirmed; corrected_source_blob_id sólo se
admite en retry_failed. Se conserva el run padre; no se crea un hijo por cliente.
Antes de POST se relee Current con el owner real y se exigen identidad, versión
y preview_hash coincidentes. El GET conserva el objeto errors y su versión,
cursor y ausencia/null exactos; no convierte esos errores en un commit Original.

Apply envía una sola vez los bytes/key literales y acepta sólo el202 declarado,
con progress pending factual. El GET Original separado conserva SHA/key y el
string body/status202 del primer efecto. Los estados missing/in_progress/
unconfirmed permanecen inciertos y no autorizan reenvío. Un 409 se devuelve sin
rebase ni nueva clave. C7/Company/Fields revocados invalidan cada lectura actual.

## Restore

```sh
factuarea crm documents current "$DOCUMENT_UUID" --source-contracts --json
factuarea crm documents restore "$DOCUMENT_UUID" --source-contracts --confirmed \
  --data-file "$FIRST_RESTORE_BODY" --idempotency-key-env FACTUAREA_RESTORE_INTENT --json
```

El cuerpo exacto sólo admite expected_version y confirmed:true. Current se relee
antes del POST y su CAS debe coincidir. El owner Library sigue siendo responsable
de archivo/retained Vault/Company/C7 y la confirmación actual; ningún permiso,
share, retención, URL o copia de bytes se fabrica en el CLI. Sólo se imprime el
receipt200 autorizado con id/version, sin sustituirlo por un GET posterior.

El export143 **no publica Original GET de Restore**. Timeout, respuesta incompleta
o estado incompatible quedan UNKNOWN. El CLI se detiene sin reenviar POST y sin
interpretar Current como confirmación del efecto. Esta ausencia no se oculta con
un endpoint o receipt local.

Los controles reutilizan auth, config, errors, envelope y `Client.Do` del CLI con
opt-in `WithSingleDispatch`; el comportamiento publicado por defecto se conserva.
Raw/key no se recortan, regeneran o guardan. La opción `--idempotency-key-env`
permite mantener la clave fuera de argv/logs; los tokens entran por los canales
existentes de auth. No se han ejecutado los dos HTTP causales preparados ni API,
build, fixtures, DB, instalaciones o suites.
