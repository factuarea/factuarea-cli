# Factuarea API — full documentation

> Concatenated Markdown export of every page at https://docs.factuarea.com. Generated on demand from the source MDX (no UI components, no layout chrome). Each page begins with its title and canonical URL so LLMs can cite back to the live documentation. The API reference is rendered from the OpenAPI spec and, being identical in every language, appears once per operation. See `/llms.txt` for a curated index.

Total pages: 5.

---
# Idempotency (/guides/idempotency)



Write operations (`POST`, `PATCH`, `DELETE`) can be received multiple
times if the connection drops mid-response, your integration retries
after a timeout, or there are automatic retries in an intermediate
gateway. To prevent the same POST from creating two invoices, the API
supports the `Idempotency-Key` header.

## How it works [#how-it-works]

1. The client generates a unique key per operation (a UUID v7 is the
   recommended choice, for consistency with the API identifiers).

2. Send it as a header on the first request:

   ```http
   POST /v1/invoices
   Idempotency-Key: 01928f10-7c0e-7c4a-9b7d-2f8a6e3c1d4b
   ```

3. The API stores the result (status code, headers and body) associated
   with that key for **24 hours**.

The response returned on a replay includes the `Idempotent-Replayed: true`
header so you can distinguish it.

## Key format [#key-format]

* An **opaque string** to the server: any unique value is valid (UUID v7,
  UUID v4, ULID, nanoid, etc.).
* Length between 1 and 255 characters.

---

# Idempotencia (/es/guides/idempotency)



Las operaciones de escritura (`POST`, `PATCH`, `DELETE`) pueden recibirse
varias veces si la conexión se corta a mitad de respuesta, tu integración
reintenta tras un timeout, o hay reintentos automáticos en un gateway
intermedio. Para evitar que el mismo POST cree dos facturas, la API
admite el header `Idempotency-Key`.

## Cómo funciona [#cómo-funciona]

1. El cliente genera una clave única por operación (un UUID v7 es la
   opción recomendada, por coherencia con los identificadores de la API).

2. Envíala como header en la primera petición:

   ```http
   POST /v1/invoices
   Idempotency-Key: 01928f10-7c0e-7c4a-9b7d-2f8a6e3c1d4b
   ```

---

# Idempotència (/ca/guides/idempotency)



Les operacions d'escriptura (`POST`, `PATCH`, `DELETE`) es poden rebre
diverses vegades si la connexió es talla a mitja resposta, la teva integració
reintenta després d'un timeout, o hi ha reintents automàtics en un gateway
intermedi. Per evitar que el mateix POST creï dues factures, l'API
admet el header `Idempotency-Key`.

## Com funciona [#com-funciona]

1. El client genera una clau única per operació (un UUID v7 és l'opció
   recomanada, per coherència amb els identificadors de l'API).

---

# GET /v1/invoices — List all invoices

- **Operation ID**: `public-api.v1.invoices.list`
- **Tag**: Invoices
- **Required scope**: `invoices:read` — Read invoices.
- **Authentication**: `Authorization: Bearer <api key>`, `X-API-Key: <api key>` or an OAuth 2.1 access token.
- **Docs**: https://docs.factuarea.com/api-reference/invoices/public-api.v1.invoices.list

List your sales invoices with cursor-based pagination. Supports filtering by `status[in]`, `client_id`, `series_id`, `issued_on[gte|lte]`, and `total[gte|lte]`.

## Query parameters

- `limit` (integer, optional, min 1, max 100, default: `25`) — Number of objects to return.
- `starting_after` (string, optional, format: uuid) — Cursor for forward pagination.
- `status[in]` (string, optional) — Invoice status.

## Responses

- **200**
  - Body (`application/json`):
    - `data` (array<object (Invoice)>, required)
    - `has_more` (boolean, required)
- **401** — Missing or invalid API key.
- **429** — Rate limit exceeded.

---

# Rate limits (/guides/rate-limits)



Every API key belongs to a rate-limit tier. When you exceed it the API
answers `429` and sets `Retry-After` with the seconds to wait.

## Tiers [#tiers]

The `free` tier is what the 10-day trial gets you; paid plans raise it.

---

# Deprecated header

The separator above is a horizontal rule inside this page, and the line
that follows it is not a canonical page header, so neither of them starts
a new page.

## Reading the headers [#reading-the-headers]

`RateLimit-Remaining` tells you how many requests are left in the window.
