# CRM native CLI delivery

The existing Go CLI owns this integration: Cobra 1.10.2, Go 1.26.4,
`internal/cmd/build_generated.go`, `internal/client`, `internal/config` and
`internal/output`. No second CLI, Company, contact store, OAuth issuer or runner
is introduced. Baseline is `c4cece03536878ebf65dedbd9ccae6831fd487d3` on
`feature/dev-2-servicelevel-native-cli`.

The embedded OpenAPI consumes the owner's private development contract: 883
operations, including 288 CRM operations, with all 564 previous operations
preserved. The existing `make generate-from-file` target and generator produce
the commands. Custom operation IDs remain unchanged; their command names use
the exact qualified Router names. `internal/cmd/crm_core_native.go` supplements
only missing owner routes, and the five OAuth/original readers reuse their
existing constructors. Help and Cobra completion use the actual registered
tree; neither requires an account download.

This is a development contract with persistent CRM capabilities and ratification
OFF. It does not prove a published API, enabled tenant/plan matrix or commercial
availability. Current native authorization and capability gates still decide
each request. No release or product activation accompanies regeneration.

| Commands | Current native v1 owner | Scope |
| --- | --- | --- |
| `crm-leads index/show/create/update` | `/v1/crm/leads` | `crm_leads:read/write` |
| `crm-opportunities list/show/create/update` | `/v1/crm/opportunity` | `crm_opportunities:read/write` |
| `crm-opportunities move-stage/close/reopen` | `/v1/crm/opportunity/{opportunity}/stage/close/reopen` | `crm_opportunities:write` |
| `crm-activities index/show/create/update` | `/v1/crm/crmactivity` | `crm_activities:read/write` |
| `crm-activities transition` | `/v1/crm/crmactivity/{activity}/status` | `crm_activities:transition` |

Bodies are public JSON objects from the actual owner requests. IDs are UUID v7;
references retain public `id`/`*_id` spelling. Updates/actions retain all CAS
values supplied by the user. Backend validation and current authorization remain
authoritative. Decimal strings, large JSON numbers, Unicode, whitespace,
omitted fields and explicit nulls survive body validation without reserialization.
For typed generated flags, the serialized body is the actual first wire.

`--profile A` or `FACTUAREA_PROFILE=A` selects only A's stored credential,
including an explicitly selected `default`. A missing selected profile fails;
the generic `FACTUAREA_API_KEY` does not silently cross that boundary. Without a
selected profile, the existing environment-key convenience remains. Credential
input through the existing login/hidden prompt and keyring is unchanged.
Actual OAuth original readers retain their separate named-token-environment
mechanism; they never manufacture API-key/OAuth equivalence.

`--json` returns stable native envelopes and error metadata. Redirected stdout
also defaults to JSON; terminal core reads use an ID/version/state/name table.
Single-page reads preserve the native page envelope. All 42 paged CRM GETs
support `--all --max-pages N`: 32 use the owner's cursor and 10 use actual numeric
page metadata. Core Lead, Opportunity and Activity lists emit rows as NDJSON.
The other paged reads emit each complete native envelope as one JSON line,
retaining secondary arrays, C7, privacy fields, nulls, totals and decimal values.
Each request retains the original filters and credential. Repeated cursors,
invalid page metadata and exhausted budgets stop the traversal. An exhausted
budget returns `pagination_budget_exhausted`, `partial:true`, and the actual
`next_cursor` or `next_page`. The maximum budget is 1000 pages. Read retries
preserve the original credential and URL; redirects are never followed.

| Exit | Meaning |
| --- | --- |
| 0 | Command/read completed |
| 1 | CLI defect |
| 2 | Local usage/configuration |
| 3 | HTTP 401 |
| 4 | HTTP 403 or missing local scope |
| 5 | HTTP 400/422 or pagination budget exhausted |
| 6 | HTTP 404 |
| 7 | HTTP 429, preserving `retry_after` |
| 8 | HTTP 409/CAS/idempotency conflict |
| 9 | Definitive server/read failure |
| 10 | Read transport failure |
| 11 | Write result unconfirmed |

HTTP status wins over a missing or owner-specific error type. JSON errors retain
422 field maps, native CAS details, required scopes, request ID and Retry-After.
The actual bearer is scrubbed if a rejected response echoes it. Non-JSON server
bodies and raw transport causes are not printed as private request data.

Every write dispatches once, including 429/5xx/redirects/timeouts. A network
failure, incomplete success, 5xx or redirect yields `write_unconfirmed` and exit
11, with the actual method, path, Idempotency-Key and SHA256 of the actual body.
No automatic replay, refresh, recovery read or send occurs. A cancellation
already present before dispatch is a transport failure, since no write occurred.

For the four native original operations, `--first-body-output private.json`
optionally creates a new 0600 file containing the exact first bytes before POST.
It refuses to overwrite an existing original. Dry-run creates no such file and
does not resolve credentials or call HTTP. Recover explicitly using the
unchanged original key and either `--first-body-file private.json` or
`--original-body-sha256` copied from `write_unconfirmed` (one source only):

```sh
factuarea --profile sandbox-a --json crm originals lead-create \
  --first-body-file private.json --idempotency-key 'original attempt key'
```

Original GETs preserve `confirmed/pending/in_progress/unconfirmed/missing` and
the exact private first response body string, with the currently authorized
credential. A missing or pending receipt does not permit a second POST. An
OAuth descendant can query through its actual current issuer token; lineage and
C7 are proven by the server. The CLI invents no lineage, grant or confirmation.

`examples/crm-journey.sh` is the real Lead → Opportunity → Activity recipe. It
requires a chosen sandbox profile, actual pipeline/stage UUIDs and explicit
invocation; it never uses `--live`. Each write has a new original key and private
wire file. Any error stops the journey; an ambiguous error prints a read-only
recovery command and requires human review before continuation. The recipe has
not been executed against an application or live provider in this slice.

Source and isolated Go transport/command proofs do not certify tenant A/B
backend execution or release publication. Private generation evidence records
the complete development OpenAPI coverage and the current CLI build separately.
The exact original criteria and remaining gates are recorded in the existing
`crm-cli-delivery` change; no task counters are changed here.
