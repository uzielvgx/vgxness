#!/bin/sh
set -eu
umask 077

fail() { printf 'backup.sh: %s\n' "$1" >&2; exit 2; }

: "${BACKUP_DIR:?set an absolute existing backup directory}"
case "$BACKUP_DIR" in
  /*) ;;
  *) fail "BACKUP_DIR must be an absolute path" ;;
esac
[ -d "$BACKUP_DIR" ] || fail "BACKUP_DIR does not exist: $BACKUP_DIR"
: "${PGPASSWORD_FILE:?set the PostgreSQL password secret file}"
[ -f "$PGPASSWORD_FILE" ] || fail "PGPASSWORD_FILE is not a regular file"
temporary="$BACKUP_DIR/.current.pgd.tmp"
published="$BACKUP_DIR/current.pgd"
lock="$BACKUP_DIR/.backup.lock"
owns_temporary=0
mkdir "$lock" 2>/dev/null || fail "another backup holds $lock; if none is running (for example after a killed container), remove that directory and retry"
cleanup() {
  if [ "$owns_temporary" = 1 ]; then rm -f -- "$temporary"; fi
  rmdir -- "$lock" 2>/dev/null || :
}
trap cleanup 0
trap 'cleanup; exit 130' 1 2 15
# The lock is held, so a leftover temporary can only come from a run that was
# killed before its cleanup; remove it like the systemd unit's ExecStartPre.
[ ! -L "$temporary" ] || fail "refusing symlink at $temporary"
if [ -f "$temporary" ]; then
  printf 'backup.sh: removing incomplete dump left by an interrupted run\n' >&2
  rm -f -- "$temporary"
fi
[ ! -e "$temporary" ] && [ ! -L "$temporary" ] || fail "unexpected non-regular entry at $temporary"
owns_temporary=1

PGPASSWORD="$(cat "$PGPASSWORD_FILE")"
export PGPASSWORD
pg_dump --format=custom --no-owner --no-privileges --file="$temporary" "$PGDATABASE"
pg_restore --list "$temporary" >/dev/null
sync -f "$temporary"
sha256sum "$temporary"
mv -T -- "$temporary" "$published"
owns_temporary=0
sync -f "$published"
