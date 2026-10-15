#!/bin/sh
# Local Colima only. Synthetic data, no published host port, no NAS access.
set -eu
project=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project"
image=${1:-shiliu-rss:local-v1}
profile=${SHILIU_SMOKE_COLIMA_PROFILE:-shiliu}
context="colima-$profile"
endpoint=$(docker context inspect "$context" --format '{{.Endpoints.docker.Host}}')
case "$endpoint" in unix://*/.colima/"$profile"/docker.sock) ;; *) echo 'Expected a local Colima Unix socket' >&2; exit 1 ;; esac
docker_local() { docker --context "$context" "$@"; }
mkdir -p "$project/.local"
stage=$(mktemp -d "$project/.local/linux-smoke.XXXXXX")
name="shiliu-smoke-${stage##*.}"
volume="$name-data"
cleanup() {
  docker_local rm --force "$name" >/dev/null 2>&1 || true
  docker_local volume rm "$volume" >/dev/null 2>&1 || true
  rm -rf "$stage"
}
trap cleanup EXIT HUP INT TERM
chmod 0755 "$stage"
printf '%s\n' synthetic-linux-smoke-password-only > "$stage/password"
chmod 0444 "$stage/password"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -o "$stage/smoke" "$project/deploy/smoke-linux.go"
docker_local volume create "$volume" >/dev/null
mountpoint=$(docker_local volume inspect "$volume" --format '{{.Mountpoint}}')
colima --profile "$profile" ssh -- sudo chown 65532:65532 "$mountpoint"
colima --profile "$profile" ssh -- sudo chmod 0700 "$mountpoint"
docker_local run --rm --platform linux/amd64 --read-only --user 65532:65532 \
  --mount "type=volume,source=$volume,target=/data" \
  --mount "type=bind,source=$stage,target=/smoke,readonly" \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
  --env SHILIU_DB=/data/reader.db --env SHILIU_PASSWORD_FILE=/smoke/password "$image" init
docker_local run -d --name "$name" --platform linux/amd64 --read-only --user 65532:65532 \
  --cap-drop ALL --security-opt no-new-privileges:true --memory 512m --pids-limit 64 \
  --mount "type=volume,source=$volume,target=/data" \
  --mount "type=bind,source=$stage,target=/smoke,readonly" \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
  --env SHILIU_DB=/data/reader.db --env SHILIU_ADDR=:8080 \
  --env SHILIU_ORIGIN=http://127.0.0.1:8080 --env SHILIU_COOKIE_SECURE=false "$image" serve >/dev/null
ready() {
  attempts=0
  until docker_local exec "$name" /reader healthcheck >/dev/null 2>&1; do
    attempts=$((attempts+1))
    if [ "$attempts" -ge 20 ]; then echo 'Linux smoke readiness failed' >&2; return 1; fi
    sleep 1
  done
}
ready
docker_local exec "$name" /smoke/smoke seed
docker_local restart --timeout 15 "$name" >/dev/null
ready
docker_local exec "$name" /smoke/smoke check
docker_local inspect "$name" --format 'uid={{.Config.User}} read_only={{.HostConfig.ReadonlyRootfs}} ports={{json .NetworkSettings.Ports}}'
docker_local image inspect "$image" --format '{{.Os}}/{{.Architecture}} {{.Id}}'
