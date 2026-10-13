# CSAT y métricas de servicio desde el CLI

Los tres comandos existentes corresponden a los GET V1 nativos: `csat` → `getCrmServiceInsights`, `metrics` → `getCrmServiceMetrics` y `drill` → `getCrmServiceInsightDrillThrough`. Todos usan la credencial API key configurada y requieren conjuntamente `service_insights:read` y `customer_service:read`. El servidor revalida ACL, campos, C7 y política de privacidad actuales; esta entrega no concede acceso ni activa capacidades.

```sh
factuarea crm service-insights csat --start 2026-01-01T00:00:00.000000Z --end 2026-02-01T00:00:00.000000Z --json
factuarea crm service-insights metrics --start 2026-01-01T00:00:00.000000Z --end 2026-02-01T00:00:00.000000Z --clock_mode business --json
factuarea crm service-insights drill --start 2026-01-01T00:00:00.000000Z --end 2026-02-01T00:00:00.000000Z --clock_mode business --as_of "$observed_as_of" --predicate_hash "$observed_predicate_hash" --metric frt --page 1 --per_page 25 --json
```

Antes de usar el tercer comando, guarda `as_of` y `predicate_hash` del segundo resultado. Conserva toda la selección original, incluidos los filtros opcionales `--agent_id`, `--team_id` y `--status` cuando se enviaron. Un 409 por selección o autoridad cambiada requiere volver a revisar el dataset; el CLI no cambia el snapshot ni vuelve a consultarlo automáticamente. `as_of` no existe en el resultado CSAT y no se fabrica a partir del reloj local.

Las cohortes son `[start,end)`, con UTC de seis decimales y hasta 366 días. CSAT cuenta cierres originales con una invitación por ciclo/participante y devuelve media/mediana con cuatro decimales. El análisis usa la creación original del ticket y su ciclo actual, con reloj `natural` o `business` explícito. FRT/MTTR tienen una muestra por contribuyente; NRT y cumplimiento SLA pueden tener varias. Backlog cuenta tickets, mientras SLA cuenta evaluaciones: sus contadores no son duraciones ni importes.

La salida conserva el JSON nativo y sus enteros/decimales exactos. Cuando `n=0`, las medias y medianas permanecen `null`; `0.000000` sólo representa una duración medida igual a cero. El CLI rechaza denominadores incoherentes, puntuaciones CSAT fuera de 1–5, medias fabricadas para un grupo vacío o valores visibles que contradicen las máscaras. Un campo oculto continúa `null`, sin convertirse en cero ni derivarse de otro contador. La política de tamaño mínimo de grupo pertenece al servidor y no se deduce en el CLI.

El drill usa `page`/`per_page` nativos y conserva las identidades actualmente autorizadas. El servidor aplica 1/25 cuando esos parámetros se omiten. La cantidad de filas debe corresponder a esa página del total autorizado; una página posterior al final es vacía. No hay cursor ni recorrido automático. Las filas sólo contienen `id`, `version`, `cycle`, `label`, `status` y `sample_count`, con los `null` autorizados conservados.

Estos tres GET no publican moneda, importes, FX, export, encuestas ni rankings automáticos. Las cabeceras de empresa, los cuerpos y las claves de efectos no forman parte de sus consultas. Los errores mantienen status/códigos/detalles/request_id y los exit codes actuales; una lectura hace un solo intento y no usa fallback SPA.

El cierre Source de este boundary corresponde a CRM-217 («Define cohortes, denominadores, media/mediana y reloj laboral vs natural; casos duplicados y ciclos son decisiones declaradas») y CRM-218 («Drill-through, export, filtros y ACL usan mismo predicado; agente/equipo/moneda no mezclan permisos ni ranking automático») únicamente para los tres GET CLI existentes. Sus pruebas HTTP offline no certifican SQL, runtime del backend, export ni el cierre global de `crm-csat-service-insights` o `crm-cli-delivery`.
