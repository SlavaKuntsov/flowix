#!/usr/bin/env bash
# Flowix end-to-end check: upload → transcode → HLS (Phase 5 nginx-vod JIT acceptance).
#
# Two modes:
#   Full pipeline (default): register → login → upload sample → poll ready → assert HLS.
#   Phase-5 only:            VIDEO_ID=<id> ./scripts/e2e.sh   # skip upload, assert HLS for a ready video.
#
# Gateway is a stub until Phase 6, so services are hit directly on their own ports.
# Override any endpoint via env, e.g. VOD=http://localhost:8080 once the gateway proxies /hls.
#
#   make up            # bring the stack up first
#   make e2e           # or: bash scripts/e2e.sh
set -euo pipefail

AUTH=${AUTH:-http://localhost:8001}
UPLOAD=${UPLOAD:-http://localhost:8003}
METADATA=${METADATA:-http://localhost:8002}
VOD=${VOD:-http://localhost:8081}

EMAIL=${EMAIL:-user@example.com}
PASSWORD=${PASSWORD:-string}
SAMPLE=${SAMPLE:-}
VIDEO_ID=${VIDEO_ID:-}
POLL_TIMEOUT=${POLL_TIMEOUT:-240}
POLL_INTERVAL=${POLL_INTERVAL:-3}
EXPECTED_RENDITIONS=${EXPECTED_RENDITIONS:-3}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say()  { echo "[e2e] $*"; }
fail() { echo "[e2e] FAIL: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing dependency: $1"; }

# http_get URL -> prints status code, writes body to $TMP/body
http_get() { curl -s -o "$TMP/body" -w '%{http_code}' "$1"; }

# resolve_url BASE_URL LINE -> absolute URL for a manifest/segment reference
resolve_url() {
  local base=$1 line=$2
  case "$line" in
    http://*|https://*) echo "$line" ;;
    /*)                 echo "${VOD}${line}" ;;                       # absolute path
    *)                  echo "${base%/*}/${line}" ;;                  # relative to manifest dir
  esac
}

need curl
need jq

say "endpoints: auth=$AUTH upload=$UPLOAD metadata=$METADATA vod=$VOD"

# ── 1. health ─────────────────────────────────────────────────────────────
say "1) health"
[ "$(http_get "$VOD/health")" = "200" ] || fail "nginx-vod not healthy at $VOD/health"
say "   nginx-vod: ok"
if [ -z "$VIDEO_ID" ]; then
  for pair in "auth:$AUTH" "upload:$UPLOAD" "metadata:$METADATA"; do
    name=${pair%%:*}; url=${pair#*:}
    [ "$(http_get "$url/health")" = "200" ] || fail "$name not healthy at $url/health"
    say "   $name: ok"
  done
fi

# ── 2. auth + upload (skipped when VIDEO_ID is provided) ───────────────────
if [ -z "$VIDEO_ID" ]; then
  say "2) auth: register/login ($EMAIL)"
  code=$(curl -s -o "$TMP/body" -w '%{http_code}' -X POST "$AUTH/api/v1/auth/register" \
    -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
  if [ "$code" = "409" ]; then
    code=$(curl -s -o "$TMP/body" -w '%{http_code}' -X POST "$AUTH/api/v1/auth/login" \
      -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
  fi
  [ "$code" = "200" ] || [ "$code" = "201" ] || fail "auth failed ($code): $(cat "$TMP/body")"
  TOKEN=$(jq -r '.access_token' < "$TMP/body")
  [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || fail "no access_token in auth response"
  say "   token acquired"

  if [ -z "$SAMPLE" ]; then
    need ffmpeg
    SAMPLE="$TMP/sample.mp4"
    say "   generating 6s 1280x720 sample via ffmpeg"
    ffmpeg -y -loglevel error \
      -f lavfi -i "testsrc=size=1280x720:rate=30:duration=6" \
      -f lavfi -i "sine=frequency=440:duration=6" \
      -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest "$SAMPLE" \
      || fail "ffmpeg sample generation failed"
  fi
  [ -f "$SAMPLE" ] || fail "sample not found: $SAMPLE"

  say "3) upload $SAMPLE"
  code=$(curl -s -o "$TMP/body" -w '%{http_code}' -X POST "$UPLOAD/api/v1/videos/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@$SAMPLE;type=video/mp4" -F "title=e2e-sample")
  [ "$code" = "201" ] || fail "upload failed ($code): $(cat "$TMP/body")"
  VIDEO_ID=$(jq -r '.id' < "$TMP/body")
  [ -n "$VIDEO_ID" ] && [ "$VIDEO_ID" != "null" ] || fail "no video id in upload response"
  say "   video_id=$VIDEO_ID"

  say "4) poll status (timeout ${POLL_TIMEOUT}s)"
  deadline=$(( $(date +%s) + POLL_TIMEOUT ))
  status=""
  while :; do
    [ "$(http_get "$METADATA/api/v1/videos/$VIDEO_ID")" = "200" ] || fail "metadata get failed"
    status=$(jq -r '.status' < "$TMP/body")
    say "   status=$status"
    [ "$status" = "ready" ] && break
    [ "$status" = "failed" ] && fail "transcode reported failed"
    [ "$(date +%s)" -ge "$deadline" ] && fail "timed out waiting for ready (last=$status)"
    sleep "$POLL_INTERVAL"
  done
else
  say "2) VIDEO_ID provided ($VIDEO_ID) — skipping auth/upload/poll"
fi

# ── 5. HLS assertions (Phase 5 acceptance) ─────────────────────────────────
MASTER_URL="$VOD/hls/$VIDEO_ID/master.m3u8"
say "5) HLS master: $MASTER_URL"
[ "$(http_get "$MASTER_URL")" = "200" ] || fail "master.m3u8 not 200: $(cat "$TMP/body")"
cp "$TMP/body" "$TMP/master.m3u8"

streams=$(grep -c '#EXT-X-STREAM-INF' "$TMP/master.m3u8" || true)
say "   EXT-X-STREAM-INF count=$streams (want $EXPECTED_RENDITIONS)"
[ "$streams" = "$EXPECTED_RENDITIONS" ] || fail "expected $EXPECTED_RENDITIONS renditions, got $streams"

# first variant playlist = first non-comment line after a STREAM-INF tag
variant=$(grep -A1 '#EXT-X-STREAM-INF' "$TMP/master.m3u8" | grep -vE '^(#|--)' | head -1 | tr -d '\r')
[ -n "$variant" ] || fail "no variant playlist line in master"
VARIANT_URL=$(resolve_url "$MASTER_URL" "$variant")
say "   variant: $VARIANT_URL"
[ "$(http_get "$VARIANT_URL")" = "200" ] || fail "variant playlist not 200"
cp "$TMP/body" "$TMP/variant.m3u8"
grep -q '#EXTINF' "$TMP/variant.m3u8" || fail "variant playlist has no #EXTINF segments"

target=$(grep -oE '#EXT-X-TARGETDURATION:[0-9]+' "$TMP/variant.m3u8" | grep -oE '[0-9]+' | head -1 || true)
say "   target segment duration=${target:-?}s (vod_segment_duration=2000ms → ~2s)"

# first segment reference
seg=$(grep -vE '^#' "$TMP/variant.m3u8" | grep -vE '^\s*$' | head -1 | tr -d '\r')
[ -n "$seg" ] || fail "no segment reference in variant playlist"
SEG_URL=$(resolve_url "$VARIANT_URL" "$seg")
say "   segment: $SEG_URL"
[ "$(http_get "$SEG_URL")" = "200" ] || fail "segment not 200"
cp "$TMP/body" "$TMP/seg.bin"
[ -s "$TMP/seg.bin" ] || fail "segment is empty"

if command -v ffprobe >/dev/null 2>&1; then
  # MPEG-TS lists the stream under [PROGRAM] and again as top-level [STREAM] — take the first non-empty line.
  codec=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name -of csv=p=0 "$TMP/seg.bin" 2>/dev/null | tr -d '\r' | awk 'NF{print; exit}' || true)
  say "   ffprobe segment video codec=${codec:-unknown}"
  [ "$codec" = "h264" ] || fail "segment does not decode as h264 (got '${codec:-none}')"
else
  say "   ffprobe not found — skipping segment decode check"
fi

say "PASS: video=$VIDEO_ID master has $streams renditions, segments serve & decode"
