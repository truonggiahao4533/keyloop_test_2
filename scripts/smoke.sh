#!/usr/bin/env bash
# End-to-end smoke test against a running stack (docker compose up -d):
# health, list, aging, log action (+ replay), history, summary, settings, RBAC.
# Tokens are minted inside the api container unless VIEWER_TOKEN /
# MANAGER_TOKEN are provided.
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
command -v jq >/dev/null || { echo "smoke: jq is required" >&2; exit 1; }

mint() { docker compose exec -T api /api token -role "$1" -sub "smoke-$1"; }
VIEWER_TOKEN="${VIEWER_TOKEN:-$(mint viewer)}"
MANAGER_TOKEN="${MANAGER_TOKEN:-$(mint manager)}"

pass=0
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok   $*"; pass=$((pass + 1)); }

# req METHOD PATH TOKEN [BODY] [EXTRA_CURL_ARGS...] -> sets $status and $body
req() {
  local method=$1 path=$2 token=$3 data=${4:-}
  shift $(( $# < 4 ? $# : 4 ))
  local args=(-s -o /tmp/smoke.$$ -w '%{http_code}' -X "$method" "$API_URL$path")
  [[ -n $token ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n $data ]] && args+=(-H 'Content-Type: application/json' -d "$data")
  status=$(curl "${args[@]}" "$@")
  body=$(cat /tmp/smoke.$$)
  rm -f /tmp/smoke.$$
}
expect() { [[ $status == "$1" ]] || fail "$2: status $status, want $1: $body"; ok "$2 ($status)"; }

req GET /healthz ""
expect 200 "healthz"
req GET /readyz ""
expect 200 "readyz"

req GET /v1/vehicles ""
expect 401 "list without token"

req GET "/v1/vehicles?limit=5" "$VIEWER_TOKEN"
expect 200 "list vehicles"
[[ $(jq '.items | length' <<<"$body") -gt 0 ]] || fail "list returned no vehicles (is the database seeded?)"
cursor=$(jq -r '.next_cursor' <<<"$body")
req GET "/v1/vehicles?limit=5&cursor=$cursor" "$VIEWER_TOKEN"
expect 200 "list next page"

req GET "/v1/vehicles?make=Toyota&min_age_days=30&max_age_days=200" "$VIEWER_TOKEN"
expect 200 "list with filters"
jq -e 'all(.items[]; .make == "Toyota" and .age_days >= 30 and .age_days <= 200)' <<<"$body" >/dev/null || fail "filters not applied"

threshold=$(curl -s -H "Authorization: Bearer $VIEWER_TOKEN" "$API_URL/v1/settings/aging-threshold" | jq .days)
req GET "/v1/vehicles/aging?limit=10" "$VIEWER_TOKEN"
expect 200 "aging stock"
jq -e --argjson t "$threshold" 'all(.items[]; .is_aging and .age_days > $t)' <<<"$body" >/dev/null || fail "aging list contains non-aging vehicles"
vehicle_id=$(jq -r '.items[0].id' <<<"$body")
[[ $vehicle_id != null ]] || fail "no aging vehicles"

key="smoke-$(date +%s)-$RANDOM"
action='{"action_type":"PRICE_REDUCTION_PLANNED","note":"smoke test"}'
req POST "/v1/vehicles/$vehicle_id/actions" "$VIEWER_TOKEN" "$action" -H "Idempotency-Key: $key"
expect 403 "viewer cannot log action"
req POST "/v1/vehicles/$vehicle_id/actions" "$MANAGER_TOKEN" "$action" -H "Idempotency-Key: $key"
expect 201 "log action"
action_id=$(jq -r .id <<<"$body")
req POST "/v1/vehicles/$vehicle_id/actions" "$MANAGER_TOKEN" "$action" -H "Idempotency-Key: $key"
expect 201 "replay action"
[[ $(jq -r .id <<<"$body") == "$action_id" ]] || fail "replay returned a different action"
req POST "/v1/vehicles/$vehicle_id/actions" "$MANAGER_TOKEN" '{"action_type":"PRICE_REDUCED"}' -H "Idempotency-Key: $key"
expect 409 "key reuse with different body"

req GET "/v1/vehicles/$vehicle_id/actions" "$VIEWER_TOKEN"
expect 200 "action history"
[[ $(jq -r '.items[0].id' <<<"$body") == "$action_id" ]] || fail "latest history entry is not the logged action"

req GET /v1/dashboard/summary "$VIEWER_TOKEN"
expect 200 "dashboard summary"
jq -e '.total_in_stock > 0 and .aging_count == ([.aging_by_make[].count] | add // 0)' <<<"$body" >/dev/null || fail "summary is inconsistent: $body"

req PUT /v1/settings/aging-threshold "$MANAGER_TOKEN" "{\"days\":$threshold}"
expect 200 "set threshold (unchanged value)"

echo "smoke: $pass checks passed"
