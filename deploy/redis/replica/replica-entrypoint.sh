#!/bin/sh
set -eu

# The first start establishes the isolated fixture's replication relationship.
# Later starts deliberately omit --replicaof so Sentinel can preserve or
# reconfigure the role after a failover instead of forcing the old primary
# back under the original hostname.
marker=/data/.b8-replica-initialized
redis-server --bind 0.0.0.0 --protected-mode no --appendonly yes &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' INT TERM EXIT

until redis-cli -h 127.0.0.1 -p 6379 ping >/dev/null 2>&1; do sleep 1; done
if [ ! -f "$marker" ]; then
  until redis-cli -h redis-primary -p 6379 ping >/dev/null 2>&1; do sleep 1; done
  redis-cli -h 127.0.0.1 -p 6379 replicaof redis-primary 6379 >/dev/null
  touch "$marker"
fi

wait "$pid"
