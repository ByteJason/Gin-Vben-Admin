#!/bin/sh
set -eu

states="$(mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -B -e \
  "SELECT SERVICE_STATE FROM performance_schema.replication_connection_status; SELECT SERVICE_STATE FROM performance_schema.replication_applier_status;" 2>/dev/null | tr '\n' ' ')"
[ "$states" = "ON ON " ] || [ "$states" = "ON ON" ]
