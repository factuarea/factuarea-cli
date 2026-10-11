# CRM NativeCrm134 consumer

This consumer extends the existing private native CLI transport for service stages,
ticket bulk actions and document Restore Original. The command registration,
manifest and selected schema are generated only after Root delivers a real Native149
export and its input manifest. Publication, ratification and delivery remain OFF,
with no advertised capabilities. A credential with the required scope is necessary;
the native server checks Company, current authority and feature availability.

The existing `crm-source139` extractor owns the reference closure and the existing
native context owns credential/scope lookup. `--schema` shows the actual operation
and its selected components without authentication or HTTP. `--dry-run` validates
the same body, literal key and Original selectors used by dispatch, without HTTP or
printing identities, payloads or keys.

## Commands

| Command | Native scope | Input |
| --- | --- | --- |
| `crm service-stages list` | `customer_service:read` | Optional `--pipeline-id`, `--page`, `--per-page` |
| `crm service-stages update <stage>` | `customer_service:write` | Retained PATCH body, explicit key and `--confirmed` |
| `crm service-stages original <stage>` | `customer_service:write` | The same PATCH body and key |
| `crm service-tickets bulk-actions tag-options` | `customer_service:read` | No body |
| `crm service-tickets bulk-actions apply` | `customer_service:write` | Retained POST body, explicit key and `--confirmed` |
| `crm service-tickets bulk-actions original <ticket>` | `customer_service:write` | The same bulk POST body and key; ticket must occur in its selection |
| `crm documents restore-original <document>` | `crm_documents:read` | The same Restore POST body and key |

Every command requires explicit `--source-contracts`. Mutation also uses the CLI's
existing live-environment confirmation. UUIDs are opaque native identities. No
Company, realm, authority, numeric internal ID or producer receipt is accepted from
the caller.

The Stage PATCH body contains exactly `pipeline_id`, `expected_version`,
`expected_pipeline_version`, `name`, `mapped_status` and `wip_limit` (required and
nullable). Original derives the owner's SHA256 fingerprint from the exact normalized
Stage intent; the selector is not a hash of raw JSON formatting.

The Bulk POST body contains `action`, `items` and only that action's payload. Each
item retains `id` and `expected_version`. Actions are `assign` (`assignee_id`, nullable),
`priority` (`priority`), `tags` (`tag_ids`, including an empty array to clear tags) or
`status` (`pipeline_id`, `stage_id`). Original derives that ticket's action, CAS and
payload from the retained selection. It keeps `confirmed`, `rejected` and `unknown`
separate and checks the response's per-item order, identities, counters and
`can_retry=false`.

Restore Original validates the retained native Restore POST body, including its
original `expected_version` and `confirmed: true`. It reads the owner's Original
receipt with that CAS and the same key. A confirmed response must retain the original
restore revision (`expected_version + 1`), even after Current advances or the document
is archived again. The existing Source143 Restore write command remains the producer
of that intent.

## Retaining and recovering one intent

Keep the first body file and its literal key before dispatch. A key may be provided
through `--idempotency-key-env` so it is absent from argv; ASCII surrounding whitespace
is rejected because the HTTP transport cannot preserve it literally. Bodies are read
without rewriting or updating the file, and duplicate JSON keys are rejected.

```sh
factuarea crm service-stages update "$STAGE_UUID" --source-contracts --confirmed \
  --data-file stage-intent.json --idempotency-key-env CRM_INTENT_KEY --json
factuarea crm service-stages original "$STAGE_UUID" --source-contracts \
  --data-file stage-intent.json --idempotency-key-env CRM_INTENT_KEY --json

factuarea crm service-tickets bulk-actions apply --source-contracts --confirmed \
  --data-file bulk-intent.json --idempotency-key-env CRM_INTENT_KEY --json
factuarea crm service-tickets bulk-actions original "$TICKET_UUID" --source-contracts \
  --data-file bulk-intent.json --idempotency-key-env CRM_INTENT_KEY --json

factuarea crm documents restore-original "$DOCUMENT_UUID" --source-contracts \
  --data-file restore-intent.json --idempotency-key-env CRM_INTENT_KEY --json
```

Each command dispatches once, with HTTP retries and redirects disabled. Original is
GET with no body. A lost or malformed response remains unknown: use Original with
the same retained intent and key; never resend POST/PATCH or infer confirmation from
Current. Bulk Original resolves each ticket independently and never invents an
aggregate receipt.

## Generation handoff

`extract.py --input <actual-root-manifest> --native-sha256 <actual-root-sha256>
--manifest-sha256 <actual-root-manifest-sha256>
--output-root <durable-cli_consumers134/old/post/cli>` verifies the actual artifact,
its manifest, the exact seven operations/scopes and native carriers. It reuses the
existing reference extractor and embeds the actual native and selected-schema
hashes into the generated consumer. Restore POST is a validation boundary, with no
new command or dispatch. It does not generate from a guessed contract or alter the
distributed public spec.

The actual Native149 manifest groups its disabled gates in `production`. The new
generator projects those verified fields to the existing extractor's input ABI,
records that projection's distinct SHA256 and preserves the actual Root manifest
SHA256 in the selected contract. Source139/143 extractors and pins remain unchanged.

Source preparation is separate from runtime evidence. Previous four Go and two
Python HTTP checks remain prior observed greens; lost `/tmp` raw output has not been
recovered or reproduced. This generator preparation performs no tests, app runtime,
database work, installation, Git operation or publication.
