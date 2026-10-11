# Exportación general nativa de CRM

`factuarea crm entity-exports` consume el productor nativo Source107. Los seis dueños son `organization`, `person`, `lead`, `opportunity`, `activity` y `service_ticket`. Una nota puede ser un filtro `type: "note"` de Activity. La matriz de columnas, filtros y períodos publicada por cada dueño está en `internal/spec/crm_entity_export_contract.json`; describe el esquema, nunca una autorización.

El comando reutiliza perfil, autenticación, entorno, errores y descubrimiento del CLI. El preflight consulta los scopes de la credencial actual mediante `/v1/account` y exige `crm_imports:export`. `--skip-scope-check` omite sólo esa comprobación local. El servidor conserva scopes, Company, actor, C7, máscaras, revisiones y admisión actual en solicitud, Original y descarga. El despliegue CRM continúa sujeto a su ratificación y activación; el wrapper no declara una capability nueva.

## Solicitud e identidad original

Prepara un único JSON cerrado con las cinco claves `id`, `entity_type`, `filters`, `columns` y `confirmed`. `id` es tu UUIDv7 estable en minúsculas; `filters` conserva el objeto nativo del dueño, incluidas sus claves de período; `columns` conserva una lista ordenada de columnas distintas y `confirmed` debe ser `true`.

Tras confirmar esa selección humana, envía sus bytes originales por stdin:

```sh
factuarea crm entity-exports request --data - --confirmed --idempotency-key ORIGINAL_KEY < selection.json
```

También existen `--data @selection.json` y `--data-file selection.json`. El CLI sólo lee el fichero elegido; no lo crea ni guarda la selección. Evita poner filtros con información personal directamente en los argumentos del shell.

El cuerpo se conserva sin normalización ni cambios de orden. La clave ASCII original es obligatoria y se conserva sin sustitución. No se generan UUIDs, claves, precios, CSVs, páginas ni selecciones parciales. El servidor decide la validez de valores, períodos y filtros tipados con sus definiciones vigentes; seleccionar una columna inaccesible falla de forma cerrada.

Hay un solo POST a `/v1/crm/entity-exports`: sin redirect, retry HTTP ni replay automático de transporte. `processing` llega con HTTP202; los demás estados originales llegan con HTTP200. La respuesta pública tiene `data.id`, `data.entity_type` y `data.status`; sólo `ready` añade `filename` y `size_bytes`.

## Original, estado y recuperación

```sh
factuarea crm entity-exports original ORIGINAL_UUID
factuarea crm entity-exports status ORIGINAL_UUID
```

Ambos controles hacen el mismo GET `/v1/crm/entity-exports/{export}`. El resultado factual puede ser `processing`, `ready`, `failed` o `expired`. No existe un endpoint de retry ni un reencolado implícito.

Un timeout, respuesta incompleta, redirect o fallo HTTP5xx durante la solicitud deja el resultado sin confirmar. El CLI devuelve el UUID y la instrucción de consultar Original; no imprime el cuerpo, selección ni clave y no reenvía el POST. Conserva tú mismo el UUID, cuerpo y clave originales. Consulta ese UUID hasta obtener el estado actual. Una nueva exportación requiere una nueva selección, confirmación humana y nueva identidad deliberadas.

## Descarga del mismo trabajo

```sh
factuarea crm entity-exports download ORIGINAL_UUID --output chosen.csv
factuarea crm entity-exports download ORIGINAL_UUID | consumer
```

Cada descarga consulta primero el Original actual y exige `ready`. Después hace GET `/v1/crm/entity-exports/{export}/download`. La metadata ready no concede permiso: el endpoint binario reautoriza el trabajo y los bytes originales.

El CSV fluye directamente desde el servidor, con nombre `crm-{entity_type}.csv`, `text/csv; charset=utf-8`, `private, no-store`, `nosniff` y tamaño original. El CLI comprueba esa metadata y el tamaño sin construir ni cargar el CSV completo en memoria. Los errores HTTP403/409/422 conservan el envelope nativo; no se convierten en un CSV vacío o una exportación lista.

`--output` crea únicamente el fichero nuevo elegido explícitamente, con permisos `0600`, sin sobreescribir otro fichero. Si la descarga falla, elimina el parcial. Sin ese flag, stdout requiere pipe; un fallo de transporte puede dejar bytes parciales en el consumidor. No existe caché, checkpoint automático, almacenamiento de credenciales o recibo privado adicional.

`factuarea commands --json` publica estos controles y sus tres operation IDs nativos; `status` comparte la operación Original. La especificación existente y sus exportaciones antiguas conservan su dueño. Los dos casos HTTPfake de esta entrega se prepararon como Source; su ejecución corresponde al cierre del grupo.
