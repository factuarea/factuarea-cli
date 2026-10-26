#!/usr/bin/env bash
# Receta: plantillas PDF y presentación de documentos con el CLI.
#
# Todos los comandos y flags existen en el binario generado (`factuarea <grupo>
# <acción> --help`). Pensado para correr contra un servidor de pruebas o un mock
# local; no toca cuentas reales: la key y la URL por defecto son ficticias.
#
#   FACTUAREA_BASE_URL=http://127.0.0.1:8080 bash examples/document-templates.sh
#
# Las plantillas, los artefactos y la presentación de los documentos cuelgan del
# eje de empresa (`/v1/companies/{company}/…`): `--company` fija la empresa en
# cada comando. También vale como primer argumento posicional y, si tu credencial
# alcanza una sola empresa, se resuelve sola. Lo que se escribe es el `id` de la
# empresa (`factuarea account show`, `data.scope[].id`), nunca su NIF.
#
# `--skip-scope-check` evita la consulta previa de scopes (un mock no la sirve);
# con una cuenta real puedes quitarlo.
set -euo pipefail

export FACTUAREA_API_KEY="${FACTUAREA_API_KEY:-fact_test_aaaaaaaaaaaaaaaaaaaaaaaa}"
export FACTUAREA_BASE_URL="${FACTUAREA_BASE_URL:-http://127.0.0.1:8080}"
F="${FACTUAREA_BIN:-factuarea}"
COMPANY="${COMPANY_ID:-acme_co}"
S=(--company "$COMPANY" --skip-scope-check)
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

TEMPLATE="${TEMPLATE_ID:-00000000-0000-7000-8000-000000000001}"
INVOICE="${INVOICE_ID:-00000000-0000-7000-8000-000000000002}"
SERIES="${SERIES_ID:-00000000-0000-7000-8000-000000000003}"
SUBMISSION="${SUBMISSION_ID:-00000000-0000-7000-8000-000000000004}"

# 1. Crear una plantilla con flags tipados; --data-file/-d aceptan el cuerpo completo.
$F pdf-templates create --name "Factura clásica" --base-preset classic "${S[@]}" --json

# 2. Actualizar con If-Match: el ETag viaja literal, tal como lo devolvió un GET.
#    --show-etag escribe el ETag a stderr y deja stdout intacto.
ETAG="$($F pdf-templates find "$TEMPLATE" --show-etag "${S[@]}" --json 2>&1 >/dev/null | sed -n 's/^etag: //p')"
$F pdf-templates update "$TEMPLATE" --name "Factura clásica 2026" --if-match "$ETAG" "${S[@]}" --json

# 3. null explícito frente a omitido o vacío:
#    --null-<campo> envía null (borra la descripción); un campo sin flag no se envía;
#    --description "" envía la cadena vacía.
$F pdf-templates update "$TEMPLATE" --null-description --if-match "$ETAG" "${S[@]}" --json
echo '{"description":null,"name":"Factura clásica","expected_version":2}' > "$WORK/body.json"
$F pdf-templates update "$TEMPLATE" --data-file "$WORK/body.json" "${S[@]}" --json

# 4. Subida multipart de un logo (campos de texto en --data, fichero en --file-<campo>).
printf '\x89PNG\r\n\x1a\n' > "$WORK/logo.png"
$F pdf-templates asset-upload --file-file "$WORK/logo.png" --data '{"kind":"logo"}' "${S[@]}" --json

# 5. Asignar la plantilla de una serie (PATCH con If-Match) y asignar la de un
#    documento; en un documento template_id es requerido y admite null (hereda).
$F series assign-pdf-template "$SERIES" --pdf-template-id "$TEMPLATE" --if-match "$ETAG" "${S[@]}" --json
$F invoices assign-presentation "$INVOICE" --null-template-id --expected-version 1 "${S[@]}" --json

# 6. PDF asíncrono: prepare -> render -> sondeo. Un 202 imprime el JSON de estado
#    y Retry-After en stderr sin crear fichero; un 200 escribe los bytes del PDF.
ARTIFACT="$($F pdf-artifacts prepare --document-type invoice --document-id "$INVOICE" \
  --presentation-id "$TEMPLATE" --purpose complete --format a4 --variant official "${S[@]}" --json |
  sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p' | head -n1)"
$F pdf-artifacts render "$ARTIFACT" --data '{}' "${S[@]}" --json
for attempt in $(seq 1 12); do
  $F pdf-artifacts download "$ARTIFACT" -o "$WORK/factura.pdf" --show-etag "${S[@]}" 2>"$WORK/download.err" &&
    [ -s "$WORK/factura.pdf" ] && break
  sleep 2
done
PDF_ETAG="$(sed -n 's/^etag: //p' "$WORK/download.err")"

# 7. Descarga condicional: con el ETag guardado, un 304 sale con exit 0, sin
#    salida y sin tocar el fichero existente.
$F pdf-artifacts download "$ARTIFACT" -o "$WORK/factura.pdf" --if-none-match "$PDF_ETAG" --show-etag "${S[@]}"

# 8. XML Facturae exacto: el de un intento propio, byte a byte.
$F invoices facturae "$INVOICE" --submission_id "$SUBMISSION" -o "$WORK/factura.xml" "${S[@]}"

# 9. Envío a FACe: UNA sola llamada, sin reintento automático (un envío incierto
#    no se repite a ciegas). Si falla, consulta el historial antes de decidir.
$F invoices face-submissions submit "$INVOICE" --confirm "$INVOICE" "${S[@]}" --json ||
  $F invoices face-submissions list "$INVOICE" "${S[@]}" --json

# 10. Listados por offset (page/per_page): una petición por llamada, sin --paginate.
$F invoices list-presentations "$INVOICE" --page 1 --per_page 20 "${S[@]}" --json
