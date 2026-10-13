Native OAuth CRM and original recovery

Choose the actual native OAuth bearer credential. API keys continue to use their existing factory/configuration. Never put an API key into the OAuth token option or synthesize a token-family identifier.

| Read | Native operation | Fine scope | OAuth grant |
| --- | --- | --- | --- |
| GET /v1/crm/credential-context | crm_access_context.show | crm_access_context:read | crm_access_context.read |
| GET /v1/crm/leads/create/original | crm_leads.create | crm_leads:write | crm_leads.write |
| GET /v1/crm/opportunity/create/original | crm_opportunities.create | crm_opportunities:write | crm_opportunities.write |
| GET /v1/crm/opportunity/{opportunity}/stage/original | crm_opportunities.move_stage | crm_opportunities:write | crm_opportunities.write |
| GET /v1/crm/crmactivity/create/original | crm_activities.create | crm_activities:write | crm_activities.write |

Credential context requires actual OAuth. It returns native company, credential and client UUIDs, issuer environment, fine scopes and authorization_version. It contains no API-key account projection or client-generated family/permission claims.

Each original GET sends only the original Idempotency-Key and original_body_sha256 query. Retain the exact first UTF-8 JSON object body: whitespace, key order, decimal strings, omitted fields and explicit nulls all contribute to SHA256. Never recompute it from parsed JSON. An opportunity-stage lookup also retains its native opportunity UUID.

The original receipt state is confirmed, pending, in_progress, unconfirmed or missing. Confirmed retains the original 2xx status and exact JSON response body as a string. Every other state retains a null original_response. Receipt validation rejects a different body hash, unknown state or fabricated confirmation. A lookup never generates a new key or emits a mutation, including after 401, 429, timeout, cancellation, lost response or OAuth refresh.

Refresh remains an explicit caller/issuer action. Supply the current access token for the next GET; the server proves completed native refresh ancestry and current authority. A distinct API key remains a real singleton credential and receives no invented OAuth family.

HTTP transports normalize whitespace at the edges of header values. These clients reject such Idempotency-Key inputs before dispatch rather than silently trimming them. Printable ASCII keys of 1–255 bytes with internal spaces are preserved.

The CLI now regenerates from the owner's private development OpenAPI through the existing `make generate-from-file` target. Exact native OAuth/original IDs delegate to the same constructors above; their retained key, body hash, current bearer and response validation remain unchanged. The four original GETs require current writer scopes and remain reads. This development contract keeps persistent CRM capabilities and ratification OFF; it does not prove publication, tenant/plan availability or deployment.
CLI usage

```sh
factuarea --json crm credential-context --oauth-token-env FACTUAREA_OAUTH_TOKEN
factuarea --json crm originals lead-create --first-body-file first-body.json --idempotency-key 'first original key' --oauth-token-env FACTUAREA_OAUTH_TOKEN
factuarea --json crm originals opportunity-stage 01900000-0000-7000-8000-000000000001 --first-body-file stage-first-body.json --idempotency-key 'stage original key' --oauth-token-env FACTUAREA_OAUTH_TOKEN
```

The command reads the named environment variable as an actual OAuth access token. It bypasses API-key profile resolution and /account preflight. Omit --oauth-token-env for the four original GETs to use the existing actual API-key resolution. Credential-context requires OAuth. --profile selects API keys and cannot accompany OAuth. --api-version preserves a chosen native version header. No live-write permission is needed for these reads.

The five commands appear in commands discovery with their existing native operations and fine scopes, all marked non-mutating. Help and shell completion derive from registered Cobra commands and flags. --json retains the response envelope and exact original body string; terminal output shows current identity or receipt state/hash/status.

Repository recognition (original delivery task1.1)

Actual checkout: /home/nando/Proyectos/factuarea-app-worktrees/dev-2-crm/development/factuarea-cli-servicelevel-native. Own branch: feature/dev-2-servicelevel-native-cli. Baseline: c4cece03536878ebf65dedbd9ccae6831fd487d3.

Loaded workspace AGENTS.md/REMOTE-SETUP.md, app AGENTS.md and applicable backend/contract rules before editing. The three SDK/CLI checkouts contain no local AGENTS.md/CLAUDE.md; inherited workspace instructions apply. PHP also follows docs/REGENERATION.md and confines handwritten code to src/Custom. Tooling: Go1.26.4; Cobra1.10.2; existing internal spec/parser generator; JSON and terminal tables; isolated Go1.26.4 installed privately.

The original 564-operation embedded input had SHA256 4df6338486fda7c911287891d5978b174ca478dd3e8b474d1dde640219a4049c. Regeneration preserves every previous method, path and operation ID and adds the native development contract. The private delivery evidence records the exact exported input, normalized embedded input and generated-source hashes. Package versions remain unchanged; no release or publication is claimed. The five native reads above retain their current OAuth identity and read-only recovery contracts.
