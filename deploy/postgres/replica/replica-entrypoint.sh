#!/bin/bash
set -euo pipefail

export PGDATA="${PGDATA:-/var/lib/postgresql/18/docker}"

until pg_isready -h pg-primary -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do sleep 2; done
mkdir -p "$PGDATA"
rm -rf "$PGDATA"/*
PGPASSWORD=replicator_password pg_basebackup -h pg-primary -U replicator -D "$PGDATA" -Fp -Xs -P -R
chmod 700 "$PGDATA"
exec postgres -D "$PGDATA" -c listen_addresses='*' -c hot_standby=on
