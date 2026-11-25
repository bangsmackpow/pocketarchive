#!/bin/sh
# Creates the non-superuser application role and the archive database.
# The engine's FORCE ROW LEVEL SECURITY policies are only effective when the
# application does not connect as a superuser or a BYPASSRLS role.
set -eu

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set app_password="$POSTGRES_PASSWORD" <<'EOSQL'
CREATE ROLE archive_app
  LOGIN PASSWORD :'app_password'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
CREATE DATABASE archive OWNER archive_app;
EOSQL

echo "pocketarchive: created archive_app role and archive database"
