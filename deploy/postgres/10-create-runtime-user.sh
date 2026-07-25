#!/usr/bin/env bash
set -Eeuo pipefail

: "${POSTGRES_APP_USER:?POSTGRES_APP_USER is required}"
: "${POSTGRES_APP_PASSWORD:?POSTGRES_APP_PASSWORD is required}"

if [ "$POSTGRES_APP_USER" = "$POSTGRES_USER" ]; then
	echo "POSTGRES_APP_USER must differ from POSTGRES_USER" >&2
	exit 1
fi

psql -v ON_ERROR_STOP=1 \
	--username "$POSTGRES_USER" \
	--dbname "$POSTGRES_DB" \
	--set=app_user="$POSTGRES_APP_USER" \
	--set=app_password="$POSTGRES_APP_PASSWORD" <<'EOSQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'app_user', :'app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app_user')
\gexec
EOSQL