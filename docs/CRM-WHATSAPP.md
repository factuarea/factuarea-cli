Native WhatsApp conversation preparation and original recovery

The current native catalog admits these conversation operations through an actual API key. It does not emit OAuth conversation grants. These clients do not turn OAuth tokens into API keys or infer conversations.read/write. Current OAuth credential context and the four CRM originals remain available through their existing native OAuth API.

| Method | Native v1 path | operationId | Native operation | Fine scope |
| --- | --- | --- | --- | --- |
| GET | /v1/crm/conversation/{conversation}/messages/{message}/whatsapp-cloud/preparation | prepareCrmWhatsAppMessage | conversations.send | conversations:write |
| POST | /v1/crm/conversation/{conversation}/messages/{message}/whatsapp-cloud/send | sendCrmWhatsAppMessage | conversations.send | conversations:write |
| GET | /v1/crm/conversation/{conversation}/messages/{message}/whatsapp-cloud/original | getCrmWhatsAppMessageOriginal | conversations.get | conversations:read |

Conversation, message and effect identifiers are real native UUIDv7. Preparation sends purpose contact_request or marketing and an optional explicit template selection: id, name, language, category and parameters. The server validates the live approved template, consent, inbound window, current company/actor/credential/environment/C7 and record permissions. A preparation token contains a server seal; it is not a client permission or family claim.

SEND retains exactly seven JSON keys: conversation_id, effect_id, expected_version, message_version, purpose, template (explicit null or the full selection), preparation_token. Message text and phone numbers remain server-owned. The first UTF-8 JSON wire is captured once, including whitespace, key order and Unicode. No HTTP Idempotency-Key is generated: the sealed native effect identity is the ledger key. SEND disables automatic retries and redirects. A timeout, transport failure, 5xx or malformed success is unconfirmed; preserve the intent and use GET original. A second SEND of that retained effect is rejected locally even after a failed or unconfirmed outcome.

GET original sends only original_wire, URL-encoded from the exact first public POST bytes, with an empty body and no key header. The returned conversation_id/message_id/effect_id, original_wire_hash and preparation_digest must match the intent. Its factual state is not_found, confirmed, unconfirmed or failed. not_found has a null receipt; the other states retain a matching receipt with status, provider_message_id, submitted_at and reason_code. Confirmed means provider transport acceptance; it does not establish delivery or reading.

The original owner requires the exact current credential UUID and C7 binding. It does not grant visibility to a different OAuth refresh successor UUID or another API key through a family adapter. Revalidation is an explicit GET with the same preparation token, effect, purpose and template; it cannot create a replacement effect or enable replay. No client automatically refreshes, revalidates, re-prepares or resends after an uncertain result.

Generated files and frozen OpenAPI generator snapshots are unchanged. These handwritten models and route methods follow the native Source descriptors and closed schemas; this delta does not claim a package release or full CRM inventory certification.

CLI usage

```sh
# FACTUAREA_API_KEY is supplied by the normal credential resolver; use native IDs.
factuarea crm whatsapp prepare "$CONVERSATION_ID" "$MESSAGE_ID" --purpose contact_request --intent-file whatsapp-intent.json
# Review the actual preparation returned by GET. SEND needs the exact message UUID confirmation.
factuarea crm whatsapp send --intent-file whatsapp-intent.json --confirm "$MESSAGE_ID"
factuarea crm whatsapp original --intent-file whatsapp-intent.json
# Explicit revalidation keeps the same sealed token/effect and exact checkpoint bytes.
factuarea crm whatsapp prepare "$CONVERSATION_ID" "$MESSAGE_ID" --purpose contact_request --intent-file whatsapp-intent.json --revalidate
```

Preparation creates a new regular 0600 checkpoint without overwriting an existing effect. It retains the full native preparation, first_wire, wire_hash, an opaque hash of the real credential/server/version and dispatched=false. Optional --template-file contains the exact template selection; optional --first-body-file is validated against all seven prepared fields. Revalidation does not rewrite the checkpoint.

SEND takes an exclusive local checkpoint lock, requires --confirm with the prepared message UUID and the normal --live acknowledgement for live credentials, validates the original context, then durably records dispatched=true before HTTP. That flag survives CLI restarts and prevents any second SEND, including after 401/403/409/422/429 or an uncertain outcome. The checkpoint contains no raw API key, but includes private message preparation/token data; keep its 0600 permissions. --api-version must match the checkpoint context.

--json preserves the native response envelope. Terminal tables show factual state/effect without dumping preparation tokens or message text. Discovery/help/completion expose the three real native operations and scopes; only SEND is mutating and irreversible. The four existing crm originals commands remain read-only and retain their original key/body SHA.
