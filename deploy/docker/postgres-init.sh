#!/bin/sh
set -eu

password() { IFS= read -r value < "$1" || [ -n "$value" ]; printf '%s' "$value"; }
valid() { case "$1" in *[!A-Za-z0-9._~-]*|'') return 1;; esac; [ "${#1}" -ge 32 ]; }
admin_password="$(password /run/secrets/postgres_admin_password)"
app_password="$(password /run/secrets/syncd_password)"
backup_password="$(password /run/secrets/backup_password)"
valid "$admin_password" && valid "$app_password" && valid "$backup_password" || exit 2
PGOPTIONS='-c log_statement=none -c log_min_error_statement=fatal' psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE ROLE vgxness_syncd LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION PASSWORD '$app_password';
CREATE ROLE vgxness_syncd_backup LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION PASSWORD '$backup_password';
ALTER DATABASE vgxness_sync OWNER TO vgxness_syncd;
ALTER SCHEMA public OWNER TO vgxness_syncd;
GRANT USAGE ON SCHEMA public TO vgxness_syncd_backup;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE vgxness_sync TO vgxness_syncd_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO vgxness_syncd_backup;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO vgxness_syncd_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE vgxness_syncd IN SCHEMA public GRANT SELECT ON TABLES TO vgxness_syncd_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE vgxness_syncd IN SCHEMA public GRANT SELECT ON SEQUENCES TO vgxness_syncd_backup;
SQL
