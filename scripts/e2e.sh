#!/usr/bin/env bash
set -euo pipefail
GATEWAY=${GATEWAY:-http://localhost:8080}
echo "[e2e] gateway=$GATEWAY"
echo "[e2e] 1) health"
curl -sf "$GATEWAY/health" || curl -sf http://localhost:8081/health || echo "health not yet"
echo "[e2e] 2) register/login"
# TODO: implement after auth service
echo "[e2e] placeholder — implement after Phase 2"
