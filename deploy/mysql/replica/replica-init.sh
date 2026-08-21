#!/bin/sh
set -eu

until mysqladmin ping -h 127.0.0.1 -u root -p"$MYSQL_ROOT_PASSWORD" --silent; do sleep 2; done
mysql -u root -p"$MYSQL_ROOT_PASSWORD" -e \
  "STOP REPLICA; RESET REPLICA ALL; CHANGE REPLICATION SOURCE TO SOURCE_HOST='mysql-primary', SOURCE_USER='replicator', SOURCE_PASSWORD='replicator_password', SOURCE_AUTO_POSITION=1; START REPLICA;"
exec tail -f /dev/null
