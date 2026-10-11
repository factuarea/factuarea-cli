# Journey CRM140: Lead → Opportunity → Activity

La receta ejecuta los comandos Source del CLI real sobre el OpenAPI nativo139,
SHA256 `438b12b53be7ae7535cd6c85ba7dea06c76d84a33024db0e0232ac8ba279be63`.
El selector copia operaciones y `$ref` desde ese artefacto. El contrato conserva
`unreleased`, `published=false`, `ratified=false`, capabilities vacías y delivery
OFF. La receta no activa CRM, publica una release ni instala paquetes.

Por defecto valida ese manifiesto y presenta un plan redactado, sin CLI, auth,
subprocesos, red o escrituras. No exige UUID ni credenciales para el plan.

```sh
python3 examplesCRMjourney140/journey.py \
  --native-input "$NATIVE_INPUT" --source-contracts --dry-run
```

Root prepara dos credenciales **nativas sandbox** de Company distintas, con los
scopes actuales de lectura de los tres owners y los scopes de escritura en A.
Sólo se leen de `FACTUAREA_JOURNEY_A_KEY` y `FACTUAREA_JOURNEY_B_KEY`. El binario
ya preparado se declara en `FACTUAREA_CLI_BIN`; el host en `FACTUAREA_BASE_URL`.
HTTPS es el valor normal. HTTP sólo admite la fixture loopback de Root. La URL
base rechaza credenciales, queries, fragmentos y prefijos de recursos.

Cada intención tiene una clave explícita distinta, sólo por entorno:
`FACTUAREA_JOURNEY_LEAD_INTENT`, `FACTUAREA_JOURNEY_OPPORTUNITY_INTENT`,
`FACTUAREA_JOURNEY_ACTIVITY_INTENT` y `FACTUAREA_JOURNEY_UPDATE_INTENT`.
Ni tokens ni claves se pasan por argv, URL o log de la receta. No hay claves
generadas, ficheros de credenciales, journal de receipts o almacenamiento PII.

## Preparación humana y ejecución deliberada

Los comandos muestran la operación/ref closure nativa sin auth ni HTTP con
`--schema --source-contracts`. No hay catálogo paralelo de cuerpos.

```sh
factuarea crm lead create --schema --source-contracts
factuarea crm opportunity create --schema --source-contracts
factuarea crm activity create --schema --source-contracts
factuarea crm activity update "$ACTIVITY_UUID" --schema --source-contracts
```

El humano prepara ficheros privados fuera del repositorio. Lead permite su ID
nativo opcional y sus datos de contacto. Opportunity exige pipeline/stage
actuales, importe decimal y moneda; `lead_id` debe ser el UUID real del Lead
anterior. Activity exige tipo/título/timezone y `target` con `type=opportunity`
y el UUID real del Opportunity. La receta no crea catálogos, actores, grants,
monedas o referencias sustitutas ni convierte un Lead por inferencia.

```sh
python3 examplesCRMjourney140/journey.py \
  --native-input "$NATIVE_INPUT" --source-contracts --run --confirmed
```

Primero relee account A/B y comprueba Company distintas y scopes actuales. Para
cada creación pide el fichero exacto y una confirmación `CREATE lead`,
`CREATE opportunity` o `CREATE activity`. Tras el éxito, consulta el Original
del dueño con el SHA de esos mismos bytes y la misma clave. Sólo su estado
`confirmed` y el mismo UUID original permiten continuar. Luego relee Current A
y exige denegación actual B (403/404, exit4/6). Current debe exponer versión real;
una máscara o ausencia detiene el flujo, sin fabricar versión1.

Los IDs nativos necesarios para preparar el siguiente fichero se muestran sólo
en el prompt humano. El resumen conserva pasos/versiones/estado, sin cuerpos,
Company, host, tokens, claves o errores recibidos. El fichero se lee de nuevo
tras cada referencia confirmada; la receta jamás cambia o reserializa sus bytes.

Al final pide un fichero PATCH de Activity con `expected_version` de Current y
`changes` nativos, y la confirmación `UPDATE activity`. El comando relee Current
inmediatamente antes del PATCH y conserva ese CAS. Si el owner cambia después
de esa lectura, su 409 (exit8) detiene el flujo y sólo se relee Current. Otro
intento requiere fichero, clave nueva y confirmación humana; no hay rebase,
reintento o resend automático. La representación Current sigue separada del
receipt original de creación.

## UNKNOWN y recuperación

Cada writer usa el mismo `Client.Do` con `WithSingleDispatch`: clave obligatoria
literal, cero retries y sin redirects ni replay de transporte. 429/5xx, timeout,
respuesta truncada o media/schema incompatibles detienen el paso. Conserva el
fichero y clave originales; no repitas `--run` para recuperarte de UNKNOWN.

```sh
python3 examplesCRMjourney140/journey.py \
  --native-input "$NATIVE_INPUT" --source-contracts \
  --recover opportunity --original-body-file "$FIRST_OPPORTUNITY_BODY"
```

`--recover` sólo ejecuta el GET Original exacto del owner, usando su clave del
entorno. No ejecuta un POST, lee Current, cambia el cuerpo ni genera intención.
`missing`, `pending`, `in_progress` y `unconfirmed` siguen factuales y no conceden
resend. El body Original conserva todos los bytes, orden JSON y saltos finales.
HTTP no conserva espacios al inicio/final de un header: esas claves se rechazan
antes de escribir en lugar de normalizarlas silenciosamente.

Activity PATCH no tiene Original GET público en139. Un UNKNOWN de ese PATCH
permanece incierto y no se confirma desde Current ni se reenvía. El contrato143
añade Apply/Restore en comandos separados; [sus límites](../../docs/CRM-SOURCE143.md)
no sustituyen los Originals de estos tres owners.

`--read --lead UUID --opportunity UUID --activity UUID` permite releer un journey
existente con A/B sin mutar. Se preparan dos casos HTTP Go (UNKNOWN/raw/Original
y CAS409/revocación) y dos unit Python (rutas/defaults/redacción/env/stdin).
No se han ejecutado receta, API, fixtures, tests, build, instalación o suites.
