#!/bin/sh
set -eu
umask 077

: "${BACKUP_DIR:?set an absolute existing backup directory}"
case "$BACKUP_DIR" in
  /*) ;;
  *) exit 2 ;;
esac
[ -d "$BACKUP_DIR" ] || exit 2
: "${PGPASSWORD_FILE:?set the PostgreSQL password secret file}"
[ -f "$PGPASSWORD_FILE" ] || exit 2
temporary="$BACKUP_DIR/.current.pgd.tmp"
published="$BACKUP_DIR/current.pgd"
lock="$BACKUP_DIR/.backup.lock"
mkdir "$lock" || exit 2
cleanup() { rmdir -- "$lock" 2>/dev/null || :; }
trap cleanup 0
trap 'cleanup; exit 130' 1 2 15
[ ! -e "$temporary" ] && [ ! -L "$temporary" ] || exit 2

PGPASSWORD="$(cat "$PGPASSWORD_FILE")"
export PGPASSWORD
pg_dump --format=custom --no-owner --no-privileges --file="$temporary" "$PGDATABASE"
pg_restore --list "$temporary" >/dev/null
sync -f "$temporary"
sha256sum "$temporary"
mv -T -- "$temporary" "$published"
sync -f "$published"
