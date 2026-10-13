# Installed native CLI proof, Batch82

The production `cmd/factuarea` entrypoint was built with the private Go1.26.4
toolchain using `go build -trimpath`. The executable is installed privately at
`../toolchains/go1.26.4/installed-batch82/factuarea` (0700 executable and directory).
This uses the existing CLI, command builder, profile store, Cobra completion,
client and output owners. No alternate CLI, generated spec or previous Source
file was written. The previous 25 Source pins were checked before and after this
group and remain unchanged.

`scripts/verify-crm-installed-batch82.sh` reproduces the bounded private build
and selects only `TestCRMInstalledBatch82` in the existing command package.
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=readonly` and the
private module/build caches prevent module download or toolchain replacement.
The shell script was syntax checked; the new test file was formatted with
`gofmt`. There was one grouped run: **21 installed-binary cases PASS**, zero
failures and zero skips. The containing Go test also passed. No old Meta17+11,
48-case helper group or previous suite was rerun.

Every test executes the actual compiled binary as a child process. Its API
fixture is a local HTTP server. Temporary XDG config/cache directories and an
explicit nonexistent private D-Bus socket isolate the user's OS keyring; native
`login --api-key -` writes only the existing 0600 fallback profile store. No
literal key is passed as a command argument. The two synthetic profiles identify
distinct HTTP fixture companies; the generic environment key cannot replace a
selected profile, and a missing profile fails without cross-profile fallback.

The installed cases cover:

- Root/core help, discovery and native flag completion without credentials or
  HTTP; bash, zsh, fish and PowerShell completion from the registered tree.
- Closed flags, UUID validation and invalid cursor budget with exit2 before HTTP;
  dry-run preserving decimal/null input without an effect or attempt file.
- Explicit opaque cursor traversal with unchanged filters and NDJSON rows;
  partial budget exhaustion exposing the exact next cursor.
- Native stage CAS bytes and HTTP403/404/409/422/429 mapped to exit4/6/8/5/7;
  field errors, scope metadata and Retry-After survive JSON output, while the
  actual request bearer is redacted. A read-only scope dispatches no write.
- A real socket closes after receiving the first POST. The binary returns
  exit11 once, emits the actual original key/hash, preserves the first body in a
  new 0600 file and exposes no private body in the error. A later explicitly
  invoked GET recovers the exact first response body without a second POST.
- A real PTY created by the system `script` utility receives the native readable
  ID/version/state/name table and strips the provider's terminal escape character.

The fixture's scopes, companies and authorization version are synthetic transport
inputs. These tests prove the compiled client consumes its actual contracts;
they do not prove application ACL, C7, OAuth lineage, database tenant isolation,
entitlements, provider approval or backend availability. No application/backend
boot, SQL, main gates, release publication or live effect occurred.

Original `crm-cli-delivery/2.4` is supported by the native known-command
help/completion proof, including zero account/cartera downloads. Original
`2.3` and `2.5` receive installed-binary evidence for their existing output and
test requirements, while their whole final coverage/sandbox criteria remain
pending. Final OpenAPI regeneration and a real authorized sandbox journey are
separate original gates. No new task leaves or counters are created here.

The machine handoff is
`../runtime-evidence/resume-20261010/cli-crm-installed-batch82-proof.json`.
It retains original criterion IDs/literals, exact new and prior Source pins,
expanded cases, the binary hash/build metadata, report hash and remaining gates.
