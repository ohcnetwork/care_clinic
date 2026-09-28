#!/bin/sh
set -eu

BACKUP_DIR=/backups
MINIO_DIR=/minio-data
CERT=/keys/backup-cert.pem
STATE_DIR=/state

RET="${DB_BACKUP_RETENTION_PERIOD:-0}"

if [ "$RET" -eq 0 ]; then
	RET_DESC="retention 0 - backups are kept forever"
else
	RET_DESC="retention ${RET}d"
fi

if [ ! -f "$CERT" ]; then
	echo "[backup] ERROR: $CERT not found - all backups must be encrypted, refusing to run"
	exit 1
fi

if [ -z "${POSTGRES_PASSWORD:-}" ]; then
	echo "[backup] ERROR: POSTGRES_PASSWORD is not set - refusing to run"
	exit 1
fi
export PGPASSWORD="$POSTGRES_PASSWORD"

DB_HOST="${POSTGRES_HOST:-db}"
DB_PORT="${POSTGRES_PORT:-5432}"
DB_USER="${POSTGRES_USER:-postgres}"
DB_NAME="${POSTGRES_DB:-care}"

# seal: plaintext file $1 -> encrypted CMS blob at $2.
seal() {
	openssl cms -encrypt -binary -aes-256-cbc -stream -outform DER -in "$1" -out "$2" "$CERT"
}

size_of() { du -h "$1" 2>/dev/null | cut -f1; }

kb_of() {
	if [ -f "$1" ]; then
		echo $(( ($(wc -c < "$1") + 1023) / 1024 ))
	else
		echo 0
	fi
}

free_kb() { df -Pk "$BACKUP_DIR" | awk 'NR == 2 { print $4 }'; }

mb() { echo "$(( $1 / 1024 )) MB"; }

latest_daily_dump() {
	latest=""
	for candidate in "$BACKUP_DIR"/care-[0-9]*.dump.enc; do
		[ -f "$candidate" ] && latest=$candidate
	done
	echo "$latest"
}

estimate_sizes() {
	dump=$(latest_daily_dump)
	if [ -n "$dump" ]; then
		stamp=${dump##*/care-}
		stamp=${stamp%.dump.enc}
		dump_kb=$(kb_of "$dump")
		files_kb=$(kb_of "$BACKUP_DIR/files-$stamp.tar.gz.enc")
		return
	fi
	db_bytes=$(PGCONNECT_TIMEOUT=5 psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
		"select pg_database_size(current_database())" 2>/dev/null || echo 0)
	case "$db_bytes" in ''|*[!0-9]*) db_bytes=0 ;; esac
	dump_kb=$(( db_bytes / 1024 ))
	files_kb=0
	if [ -d "$MINIO_DIR" ]; then
		files_kb=$(du -sk "$MINIO_DIR" 2>/dev/null | cut -f1)
		case "$files_kb" in ''|*[!0-9]*) files_kb=0 ;; esac
	fi
}

need_kb() {
	estimate_sizes
	if [ "$1" = database ]; then
		peak=$(( 2 * dump_kb ))
	else
		peak=$(( dump_kb + 2 * files_kb ))
		[ "$peak" -ge $(( 2 * dump_kb )) ] || peak=$(( 2 * dump_kb ))
	fi
	need=$(( peak * 5 / 4 + 262144 ))
	[ "$need" -ge 1048576 ] || need=1048576
	echo "$need"
}

write_status() {
	[ -d "$STATE_DIR" ] || return 0
	printf 'state=%s\nreason=%s\nat=%s\nneed_kb=%s\nfree_kb=%s\nmessage=%s\n' \
		"$1" "$2" "$(date +%s)" "${3:-0}" "${4:-0}" "${5:-}" > "$STATE_DIR/backup-status" 2>/dev/null || true
}

check_space() {
	need=$(need_kb "$1")
	free=$(free_kb)
	case "$free" in ''|*[!0-9]*)
		echo "[backup] WARNING: could not read free space for $BACKUP_DIR - trying anyway"
		return 0 ;;
	esac
	if [ "$free" -lt "$need" ]; then
		echo "[backup] ERROR: not enough space in the backup folder: this backup needs about $(mb "$need"), $(mb "$free") is free"
		SPACE_NEED=$need
		SPACE_FREE=$free
		return 1
	fi
	echo "[backup] space: $(mb "$free") free, about $(mb "$need") needed"
}

disk_full() {
	free=$(free_kb)
	case "$free" in ''|*[!0-9]*) return 1 ;; esac
	SPACE_FREE=$free
	SPACE_NEED=${SPACE_NEED:-0}
	[ "$free" -lt 262144 ] || [ "$free" -lt "$SPACE_NEED" ]
}

remove_temp() {
	if ! rm -f "$@"; then
		echo "[backup] ERROR: removing temporary backup files failed: $*"
		return 1
	fi
}

require_new_paths() {
	for backup_path in "$@"; do
		if [ -e "$backup_path" ] || [ -L "$backup_path" ]; then
			echo "[backup] ERROR: refusing to overwrite existing backup: $backup_path"
			return 1
		fi
	done
}


db_backup() {
	ts=$1
	plain="$BACKUP_DIR/.care-$ts.dump.tmp"
	sealed="$plain.enc"
	final="$BACKUP_DIR/care-$ts.dump.enc"
	require_new_paths "$final" || return 1

	echo "[backup] database: dumping $DB_NAME"
	if ! pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -Fc -f "$plain"; then
		echo "[backup] ERROR: pg_dump failed"
		remove_temp "$plain"
		return 1
	fi

	if ! pg_restore --list "$plain" >/dev/null 2>&1; then
		echo "[backup] ERROR: dump failed verification (pg_restore --list)"
		remove_temp "$plain"
		return 1
	fi
	echo "[backup] database: $(size_of "$plain") verified"

	if ! seal "$plain" "$sealed"; then
		echo "[backup] ERROR: encrypting the dump failed"
		remove_temp "$plain" "$sealed"
		return 1
	fi
	if ! remove_temp "$plain"; then
		remove_temp "$sealed"
		return 1
	fi

	if ! mv "$sealed" "$final"; then
		echo "[backup] ERROR: publishing the dump failed"
		remove_temp "$sealed"
		return 1
	fi
	echo "[backup] database: wrote $(basename "$final")"
}

files_backup() {
	ts=$1
	if [ ! -d "$MINIO_DIR" ]; then
		echo "[backup] ERROR: $MINIO_DIR is not mounted - refusing to call this a complete backup"
		return 1
	fi
	plain="$BACKUP_DIR/.files-$ts.tar.gz.tmp"
	sealed="$plain.enc"
	final="$BACKUP_DIR/files-$ts.tar.gz.enc"
	require_new_paths "$final" || return 1

	echo "[backup] files: archiving uploads"

	if ! tar_err=$(tar -czf "$plain" -C "$MINIO_DIR" . 2>&1); then
		echo "[backup] ERROR: archiving the files failed"
		[ -z "$tar_err" ] || printf '%s\n' "$tar_err" | tail -n 3 | sed 's/^/[backup]   /'
		remove_temp "$plain"
		return 1
	fi
	if [ ! -s "$plain" ] || ! tar -tzf "$plain" >/dev/null 2>&1; then
		echo "[backup] ERROR: files archive is missing or unreadable"
		remove_temp "$plain"
		return 1
	fi
	echo "[backup] files: $(size_of "$plain") verified"

	if ! seal "$plain" "$sealed"; then
		echo "[backup] ERROR: encrypting the files archive failed"
		remove_temp "$plain" "$sealed"
		return 1
	fi
	if ! remove_temp "$plain"; then
		remove_temp "$sealed"
		return 1
	fi

	if ! mv "$sealed" "$final"; then
		echo "[backup] ERROR: publishing the files archive failed"
		remove_temp "$sealed"
		return 1
	fi
	echo "[backup] files: wrote $(basename "$final")"
}


prune() {
	if ! find "$BACKUP_DIR" -maxdepth 1 -type f \( -name '.care-*.tmp*' -o -name '.files-*.tmp*' \) -delete; then
		echo "[backup] ERROR: removing abandoned temporary backup files failed"
		return 1
	fi

	if [ "$RET" -eq 0 ]; then
		echo "[backup] $RET_DESC"
		return 0
	fi
	echo "[backup] retention: deleting sets older than ${RET} days"
	if ! find "$BACKUP_DIR" -maxdepth 1 -type f \( -name 'care-*.dump*' -o -name 'files-*.tar.gz*' \) -mtime +"$RET" -delete; then
		echo "[backup] ERROR: pruning old backups failed"
		return 1
	fi
}

record_failure() {
	if disk_full; then
		echo "[backup] the backup folder ran out of space ($(mb "$SPACE_FREE") free)"
		write_status failed disk_full "$SPACE_NEED" "$SPACE_FREE" "the backup folder ran out of space"
	else
		write_status failed error 0 0 "$1"
	fi
}

run_backup() {
	ts=$(date +%Y%m%d-%H%M%S)
	echo "[backup] ===== backup set $ts (encrypted) ====="
	write_status running "" 0 0 ""

	SPACE_NEED=""
	if ! check_space set; then
		echo "[backup] backup set $ts: FAILED before writing (backup folder is full); retention skipped"
		write_status failed disk_full "$SPACE_NEED" "$SPACE_FREE" "not enough space in the backup folder"
		return 1
	fi

	if ! require_new_paths "$BACKUP_DIR/care-$ts.dump.enc" "$BACKUP_DIR/files-$ts.tar.gz.enc"; then
		echo "[backup] backup set $ts: FAILED before writing; retention skipped"
		write_status failed error 0 0 "a backup with this name already exists"
		return 1
	fi

	if ! db_backup "$ts"; then
		echo "[backup] backup set $ts: FAILED at the database step; retention skipped"
		record_failure "the database step failed"
		return 1
	fi

	if ! files_backup "$ts"; then
		echo "[backup] backup set $ts: FAILED at the files step; retention skipped"
		record_failure "the files step failed"
		return 1
	fi

	if ! prune; then
		echo "[backup] backup set $ts: published, but cleanup FAILED"
		write_status failed error 0 0 "the backup was written, but removing old backups failed"
		return 1
	fi
	echo "[backup] backup set $ts: SUCCESS"
	write_status ok "" 0 "$(free_kb)" ""
	echo "[backup] done"
}

with_backup_lock() (
	if ! flock -x 9; then
		echo "[backup] ERROR: acquiring the backup lock failed"
		return 1
	fi
	"$@"
) 9>"$BACKUP_DIR/.backup.lock"

manual_backup() {
	name=$1
	if ! require_new_paths "$BACKUP_DIR/care-$name.dump.enc" "$BACKUP_DIR/files-$name.tar.gz.enc"; then
		return 1
	fi
	if ! db_backup "$name"; then
		echo "[backup] manual backup $name: FAILED at the database step"
		return 1
	fi
	if ! files_backup "$name"; then
		echo "[backup] manual backup $name: FAILED at the files step"
		return 1
	fi
	echo "[backup] manual backup $name: SUCCESS"
	write_status ok "" 0 "$(free_kb)" ""
}

# One-shot mode, used by the app's "Backup now": database and files under the
# name the caller picked, then exit. Same dump/archive/verify/seal/rename as the
# daily run, without retention.
if [ "${1:-}" = "once" ]; then
	name=${2:?no backup name given}
	SPACE_NEED=""
	check_space set || exit 1
	if ! with_backup_lock manual_backup "$name"; then
		if disk_full; then
			echo "[backup] ERROR: the backup folder ran out of space ($(mb "$SPACE_FREE") free)"
		fi
		exit 1
	fi
	exit
fi

echo "[backup] sidecar started; encrypted backups -> $BACKUP_DIR ($RET_DESC)"

while true; do
	if ! with_backup_lock run_backup; then
		echo "[backup] backup cycle failed - see errors above"
	fi
	sleep 86400
done
