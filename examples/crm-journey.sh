#!/usr/bin/env bash
set -euo pipefail
umask 077

# Use a profile created by the CLI's existing hidden login/keyring mechanism.
# No key, password or bearer is accepted as a visible argument here.
: "${CRM_SANDBOX_PROFILE:?Select an existing sandbox profile}"
: "${CRM_PIPELINE_ID:?Use an actual authorized pipeline UUID v7}"
: "${CRM_STAGE_ID:?Use an actual authorized stage UUID v7}"
command -v factuarea >/dev/null
command -v jq >/dev/null

attempt_dir=$(mktemp -d "${TMPDIR:-/tmp}/factuarea-crm-original.XXXXXXXX")
printf 'Private original attempts: %s\n' "$attempt_dir"
cli=(factuarea --profile "$CRM_SANDBOX_PROFILE" --json --no-input)
"${cli[@]}" whoami > "$attempt_dir/account.json"

# This helper dispatches once. A failed POST stops the recipe; recovery is an
# explicit GET chosen by the human, never a shell retry or automatic send.
write_once() {
  local resource=$1 original=$2 body=$3 key=$4 result=$5
  local first="$attempt_dir/$original-first.json"
  local status=0
  "${cli[@]}" "$resource" create --data-file "$body" \
    --idempotency-key "$key" --first-body-output "$first" \
    > "$result" 2> "$attempt_dir/$original-error.json" || status=$?
  if (( status != 0 )); then
    cat "$attempt_dir/$original-error.json" >&2
    if (( status == 11 )); then
      printf 'Review the original with GET only:\n' >&2
      printf '%q ' "${cli[@]}" crm originals "$original" \
        --first-body-file "$first" --idempotency-key "$key" >&2
      printf '\nDo not issue a second POST for this ambiguous attempt.\n' >&2
    fi
    exit "$status"
  fi
}

prefix="journey-${attempt_dir##*/}"
jq -n '{name:"Synthetic CLI journey",notes:null,source:"manual"}' \
  > "$attempt_dir/lead-input.json"
write_once crm-leads lead-create "$attempt_dir/lead-input.json" \
  "$prefix-lead" "$attempt_dir/lead-response.json"
lead_id=$(jq -er '.data.id' "$attempt_dir/lead-response.json")

jq -n --arg lead "$lead_id" --arg pipeline "$CRM_PIPELINE_ID" \
  --arg stage "$CRM_STAGE_ID" \
  '{title:"Synthetic opportunity",lead_id:$lead,pipeline_id:$pipeline,stage_id:$stage,amount:"150.00",currency:"EUR"}' \
  > "$attempt_dir/opportunity-input.json"
write_once crm-opportunities opportunity-create "$attempt_dir/opportunity-input.json" \
  "$prefix-opportunity" "$attempt_dir/opportunity-response.json"
opportunity_id=$(jq -er '.data.id' "$attempt_dir/opportunity-response.json")

jq -n --arg opportunity "$opportunity_id" \
  '{type:"note",title:"Human follow-up",body:null,target:{type:"opportunity",id:$opportunity},timezone:"Europe/Madrid"}' \
  > "$attempt_dir/activity-input.json"
write_once crm-activities activity-create "$attempt_dir/activity-input.json" \
  "$prefix-activity" "$attempt_dir/activity-response.json"

"${cli[@]}" crm-leads show "$lead_id"
"${cli[@]}" crm-opportunities show "$opportunity_id"
jq '{state:.data.state,activity_id:.data.activity_id,effect_id:.data.effect_id}' \
  "$attempt_dir/activity-response.json"
