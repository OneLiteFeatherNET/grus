#!/usr/bin/env bash
# Acceptance demo of spec 8.4 against a running controller.
#
#   G=https://grus.example GRUS_ADMIN_TOKEN=... IMAGE_TAG=abc123 ./examples/demo.sh
#
# Needs curl and sed. Pool specs are rendered into a temp dir with IMAGE_TAG substituted.
#
# Spec 8.4 point 3 needs load on BOTH lobbies: the buffer rule counts empty instances, so one loaded
# lobby is not enough to trigger the third instance. The script therefore loads two lobbies.
set -euo pipefail

: "${G:?set G to the controller base URL}"
: "${GRUS_ADMIN_TOKEN:?set GRUS_ADMIN_TOKEN}"
: "${IMAGE_TAG:?set IMAGE_TAG to the image tag to deploy}"

HERE=$(cd "$(dirname "$0")" && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

AUTH=(-H "Authorization: Bearer ${GRUS_ADMIN_TOKEN}" -H "Content-Type: application/json")
POLL_ATTEMPTS=60
POLL_SECONDS=5

render() {
  sed "s|IMAGE_TAG|${IMAGE_TAG}|g" "${HERE}/$1" > "${TMP}/$1"
}

# instance_ids POOL prints the ids of the pool's instances, one per line.
instance_ids() {
  curl -fsS "${AUTH[@]}" "${G}/v1/instances?pool=$1" | grep -o '"id":"[^"]*"' | cut -d'"' -f4 || true
}

# ready_count POOL prints how many instances of the pool are Ready.
ready_count() {
  curl -fsS "${AUTH[@]}" "${G}/v1/instances?pool=$1" | grep -o '"state":"Ready"' | wc -l || true
}

# set_players ID PLAYERS sets the player count of one instance through the admin debug endpoint.
set_players() {
  curl -fsS "${AUTH[@]}" -X POST "${G}/v1/instances/$1/debug/status" \
    -d "{\"players\":$2,\"maxPlayers\":20}" > /dev/null
}

render pool-proxy.json
render pool-lobby.json

echo "== 1. PUT proxy pool (static, min=max=1)"
curl -fsS "${AUTH[@]}" -X PUT --data-binary "@${TMP}/pool-proxy.json" "${G}/v1/pools/proxy"
echo

echo "== 2. PUT lobby pool (dynamic, min=1, buffer=1)"
curl -fsS "${AUTH[@]}" -X PUT --data-binary "@${TMP}/pool-lobby.json" "${G}/v1/pools/lobby"
echo

echo "== 3. list instances"
curl -fsS "${AUTH[@]}" "${G}/v1/instances"
echo

echo "== 4. proxy log, lines about registration"
PROXY_ID=$(instance_ids proxy | head -n 1)
if [ -n "${PROXY_ID}" ]; then
  curl -fsS "${AUTH[@]}" "${G}/v1/instances/${PROXY_ID}/logs?tail=500" | grep -i register || echo "(no register line yet)"
else
  echo "(no proxy instance yet)"
fi
echo

echo "== 5. load both lobbies: 18 and 5 players (buffer rule counts empty instances)"
for attempt in $(seq "${POLL_ATTEMPTS}"); do
  mapfile -t LOBBIES < <(instance_ids lobby)
  if [ "$(ready_count lobby)" -ge 2 ]; then
    break
  fi
  echo "waiting for two Ready lobbies (attempt ${attempt})"
  sleep "${POLL_SECONDS}"
done
if [ "$(ready_count lobby)" -lt 2 ]; then
  echo "two lobbies never became Ready" >&2
  exit 1
fi
set_players "${LOBBIES[0]}" 18
set_players "${LOBBIES[1]}" 5
echo "set ${LOBBIES[0]}=18 and ${LOBBIES[1]}=5"
echo

echo "== 6. wait for three Ready lobbies (buffer adds one)"
for attempt in $(seq "${POLL_ATTEMPTS}"); do
  if [ "$(ready_count lobby)" -ge 3 ]; then
    echo "three lobbies Ready"
    break
  fi
  echo "waiting for three Ready lobbies (attempt ${attempt})"
  sleep "${POLL_SECONDS}"
done
echo

echo "== 7. clear the load on all lobbies"
mapfile -t LOBBIES < <(instance_ids lobby)
for id in "${LOBBIES[@]}"; do
  set_players "${id}" 0
done
echo "players set to 0 on: ${LOBBIES[*]}"
echo "Now wait for the pool's scaleDownDelay (2m in pool-lobby.json), then list instances again:"
echo "one lobby should drain and stop; watch the instance.state and pool.scaled events on ${G}/v1/events."
