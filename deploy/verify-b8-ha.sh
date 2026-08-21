#!/bin/sh
set -eu

# Static contract: validates the isolated compose without creating containers.
compose_file="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/compose.b8-ha.yaml"
command -v docker >/dev/null 2>&1 || { echo "docker is required for compose config validation" >&2; exit 1; }
rendered="$(docker compose -f "$compose_file" config)"

for port in 13306 13307 15432 15433 16370 16371 16379 16380 16381 16382 16383 16384 26379 26380 26381; do
  if printf '%s\n' "$rendered" | grep -q "published: \"${port}\""; then :; else
    echo "missing published B8 port: $port" >&2
    exit 1
  fi
done
echo "B8 HA compose contract passed (config-only; containers were not started)."
