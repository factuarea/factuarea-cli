
Native human Meta contract (Source batch80)
==========================================

This extension implements the seven carriers in NativeMetaHumanSurfaceCatalog::v1 and the Root-qualified OpenAPI registry. Every carrier uses operation crm_connections.configure, fine scope crm_connections:write and OAuth consent scope crm_connections.write. Client support describes the Source contract; Root's OAuth emission/current entitlement qualification remains separate, with release gates OFF and the global matrix unratified. No live Meta grant, external App Review or activated ingestion is claimed.

Paths below are relative to /v1/crm/connections/meta-lead-ads:

| Native operationId | Method/path | Carrier |
| --- | --- | --- |
| prepareCrmMetaOAuth | POST /oauth/prepare | Command |
| completeCrmMetaOAuth | POST /oauth/complete | Command |
| listCrmMetaConnectionOptions | GET /options | Query |
| getCrmMetaMapping | GET /{connectionUuid}/mappings/{formUuid} | Query |
| inspectCrmMetaMapping | POST /{connectionUuid}/mappings/{formUuid}/inspect | Query |
| publishCrmMetaMapping | PUT /{connectionUuid}/mappings/{formUuid} | Command |
| publishCrmMetaConsent | POST /{connectionUuid}/consent | Command |

Connection/form are actual lowercase UUIDv7; credential is the original UUIDv4 or UUIDv7. External Page/form/app IDs remain decimal strings of 1–32 digits. No company, billing actor, environment, scope, credential family, Review approval or C7 authorization belongs in input JSON. Authentication derives the current server context.

OAuth prepare requires exactly {page_id,confirmed:true}; OAuth complete requires exactly {state,code} from the actual human browser callback. Code is nonempty, at most 8192 UTF-8 bytes, with no ASCII control/space or DEL. The returned state and authorization URL are opaque original values: no generated callback, verifier/challenge, client ID, code, refresh family or claimed Meta PKCE support. OAuth responses are direct objects. Installation status credentials_installed only means the original encrypted credential committed; mapping and consent are independent actions. Returned Meta scopes preserve their actual order and include the native six-permission union (ads_management, leads_retrieval, pages_manage_ads, pages_manage_metadata, pages_read_engagement, pages_show_list), allowing further actual native scope strings without expansion. These external permissions are distinct from Factuarea scopes.

Options and get mapping have no body or query. Inspect requires exactly {credential_uuid,remote_form_id}; its data projection retains actual Fields object/property/list order, checksum and CAS. expected_mapping_version:null and expected_consent_version:null are explicit native absence values, never client defaults. Inspect app_review remains {available:false,reason:"app_review_evidence_unavailable"}; metadata grants no capability.

Mapping publication requires exactly {credential_uuid,remote_form_id,mapping,expected_mapping_version,expected_publication_version,fields_snapshot_checksum,confirmed:true}. Mapping entries have exactly remote_name,target,type,field_version,required,options. Empty options are {}, never []. Basic name/email/phone/mobile/company_name/notes targets require type text, null field_version and {}. Actual published custom Fields use their UUIDv7, positive version, native type and real option UUIDs; select/multi permit actual option mappings. No schema or mapping choice is guessed. The server verifies full required-field/domain coverage and stored CAS, and a changed mapping invalidates prior consent/configuration.

Consent requires exactly {credential_uuid,grants:[{stream:actualFormUuid,actions:["read"|"ingest"]}],expected_consent_version,confirmed:true}, with nonempty unique streams/actions. No grant is added implicitly. The current external Review reader is unavailable and the native request returns a closed rejection; the client neither autoapproves nor switches to a synthetic success. The Source future consent_published projection is decoded only if an actual canonical native issuer returns it; this is no present activation claim.

Other responses retain their original {data:projection} envelope in rawBody/RawBody. Native identity in inspect/mapping must match the requested connection/form/credential/remote form. Source unknown keys, invalid types, invented Review and changed bindings fail closed.

Each method performs one explicit operation. No automatic refresh, retry after 401/409/429/503/timeout, redirect, OAuth completion, mapping publication, consent publication, capture activation or recovery request follows it. No generic Idempotency-Key is generated. Unknown command outcomes remain unconfirmed; inspect POST remains a failed query. Errors retain only closed status/reason/request ID/Retry-After/unconfirmed facts, with no raw provider response, answers, code/state or credentials. A caller must retain the original human input and decide the next native observation explicitly.

Validation here is isolated offline SDK/CLI delta coverage only. The PHP application, SQL, global gates, installed/published package and live external account have not been exercised. Historical CRM-OAUTH-ORIGINALS.md and prior WhatsApp/original Source are frozen independently.

CLI Source use and owner registration
-------------------------------------

internal/crmmeta_native_batch80 exports NewClient, the closed DTOs, NewIntent/Execute and Register(root,*actual native resolver*). Its seven commands are oauth-prepare, oauth-complete, options, mapping, inspect, publish-mapping and publish-consent. Register attaches exactly one meta group to the existing crm group and rejects duplicate registration.

Root-authorized registration in internal/cmd/root.go calls Register and resolves globalsFrom(cmd), actual newCLIContext(g,"").res.APIKey or the explicit --oauth-token-env variable, rejecting API-key profile/guise on OAuth. It retains validateBaseURLTransport, actual FACTUAREA_BASE_URL plus /v1 and --api-version; global --json selects exact JSON output. Actual API-key LIVE commands also require the existing --live flag. No account preflight or invented family is added. The new external-package registration_test.go imports cmd.NewRootCmd and exercises the actual native bearer, API-key resolution, global JSON and prior command registration. The package also accepts programmatic OAuthToken(context) from the current issuer. No installed binary or package release is claimed.

Config BaseURL includes /v1. Use real APIKey or OAuthBearer/OAuthToken exclusively; no implicit credential fallback. HTTP sends bearer authorization, disables redirects/keepalive reuse, preserves the exact request body, sets GetBody=nil and calls the transport once. Same command Intent cannot be dispatched again, even through another client; no client map retains OAuth code/state indefinitely. CAS uses exact positive int64 below MaxInt64.

Registered Source commands, with actual default API-key/profile or explicit --oauth-token-env CURRENT_NATIVE_OAUTH, are:

    factuarea crm meta options --output json
    factuarea crm meta inspect "$CONNECTION_UUID" "$FORM_UUID" --body-file inspect.json --output-file inspection.json
    factuarea crm meta oauth-prepare --body-file prepare.json --confirm "$PAGE_ID" --output-file preparation.json
    factuarea crm meta oauth-complete --body-file actual-browser-completion.json --confirm oauth-complete
    factuarea crm meta publish-mapping "$CONNECTION_UUID" "$FORM_UUID" --body-file chosen-mapping.json --confirm "$FORM_UUID"
    factuarea crm meta publish-consent "$CONNECTION_UUID" --body-file chosen-consent.json --confirm "$CONNECTION_UUID"

Every mutating command needs independent --confirm: actual Page, literal oauth-complete, actual form, actual connection respectively. Query inspect needs none. Input file retains exact bytes, no omitted-CAS filling or default mapping/grants. --dry-run validates without resolving any credential or HTTP; completion preview excludes code/state. --output json preserves the entire original native response; --output-file exclusively creates a new private 0600 file. Summary preparation prints the original HTTPS authorization URL for the user to open manually; no browser action is automatic. OAuth state is retained in the private original response for manual completion, with no fabricated code. Native unavailable consent is preserved as an error.

Directed offline proof uses only go test ./internal/crmmeta_native_batch80 -count=1 with exact isolated Go1.26.4, GOPROXY=off/GOSUMDB=off and readonly modules. Gofmt touches only these new Go files. Installed binary help/cursor/sandbox/full command inventory are outside this new delta claim.

