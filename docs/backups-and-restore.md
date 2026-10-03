[Documentation index](README.md)

# Backups and restore

This guide explains the backup and restore part of the Wails backend: what is
saved, how encryption works, how a replacement clinic is prepared, and how the
application decides what to do after an interrupted restore.

The main implementation is
[`app/internal/backup/`](../app/internal/backup), wired into the clinic by
[`backupstore.go`](../app/internal/clinic/backupstore.go). Backup creation runs
through [`deployments/scripts/backup.sh`](../deployments/scripts/backup.sh).
The source-file map at the end includes every Go file in this guide's scope,
including tests.

This is an explanation of the current code, not evidence of a completed disaster
recovery exercise. All example paths, dates, identifiers, and data below are
synthetic. Do not experiment against the only copy of a clinic.

Related guides:

- [Desktop workflows](desktop-workflows.md): the operator-facing Backups page,
  restore dialog and Advanced unlock behavior.
- [Architecture](architecture.md) and [repository map](repository-map.md):
  where this subsystem fits.
- [Wails application](wails-application.md): desktop calls, authorization, jobs,
  events, and the shared-read/exclusive-write application gate.
- [Configuration and settings](configuration-and-settings.md): effective paths,
  passwords, retention settings, and changing the backup destination.
- [Clinic lifecycle](clinic-lifecycle.md): starting services, live migrations,
  worker sequencing, and deployment services.
- [Cleanup and uninstall](cleanup-and-uninstall.md): explicit deletion choices
  and preservation of recovery material.
- [Native integrations](native-integrations.md): the process runner, operating
  system credential storage, and native file behavior.
- [Development and release](development-and-release.md): images, pins, builds,
  and interpreting test results.

## 1. The mental model

A clinic has two different kinds of clinical data:

1. **The database** stores structured records in PostgreSQL.
2. **Uploaded files** live in the storage service's Docker volume. A database
   reference to an uploaded object is not the object's contents.

The storage implementation is now **Silo**, but its Compose service is still
named `minio`, and its normal data volume is still
`care-desktop_minio-data`. Backup and recovery intentionally use those existing
names. They are not instructions to rename a service or migrate a volume.

| Term | Meaning in this implementation |
| --- | --- |
| Manual backup / "Backup now" | An encrypted database dump plus an encrypted files archive, named `care-manual-<ts>` and `files-manual-<ts>`. Both must publish before it reports success. Unlike the scheduled cycle, it applies no retention. |
| Scheduled backup | The sidecar attempts a database dump and a files archive with the same timestamp. Both must publish, and cleanup must succeed, before the cycle reports success. |
| Backup set | A database dump plus an optional matching files archive. Matching names do not prove application-level consistency. |
| Restore source | The folder containing the selected dump, optional files archive, and preferably their recovery key. It can differ from the current backup destination. |
| Staging | Preparing replacement data separately from the current database and uploaded files. |
| Cutover | Stopping writers, replacing the live files when requested, and renaming databases so the staged database takes the live name. |
| Activation | Bringing the Compose stack up after a committed cutover and waiting for health. |
| Recovery journal | Local metadata describing an unfinished restore. It is not another copy of the clinical data. |
| Retained originals | The previous database and, for a DB + files restore, a separate copy of the pre-cutover uploaded files. |

A database-only restore leaves the contents of the uploaded-files volume alone.
That can still produce a mismatch between restored database references and the
files currently present. Choose the restore scope deliberately.

### Who performs which work?

```mermaid
flowchart TD
    UI["Desktop request"] --> App["Wails App boundary: authorize and coordinate"]
    App --> Clinic["Clinic"]
    Clinic --> Now["BackupNow"]
    Now --> Sidecar["Existing backup container"]
    Sidecar --> Script["backup.sh: dump, archive, encrypt, prune"]
    Clinic --> Store["backup.Store"]
    Store --> Local["Go: names, paths, key copies, journal"]
    Store --> Helpers["Docker and Compose helper processes"]
    Helpers --> PG["PostgreSQL staging and cutover"]
    Helpers --> Volumes["Staging, previous, and live files volumes"]
    Store --> Callback["Clinic-supplied staged migration callback"]
    App --> Recovery["Native recovery-file picker"]
```

The Go package does not implement a PostgreSQL server, storage server, encryption
engine, or scheduling service. It validates inputs, records local state, and
orchestrates external commands. Docker, PostgreSQL tools, OpenSSL, Python, and
the shell do the corresponding external work.

The desktop's application gate is separate from these mechanisms. It now allows
authenticated read operations under a shared `RWMutex` read gate and reserves
protected writes for the exclusive gate. `backup.Store` does not contain that
gate or independently serialize every possible caller. Likewise, the shell
backup lock described below is not a global lock for every restore, settings
change, or deletion.

## 2. Files, names, and locations

### Backup artifacts

The examples use the synthetic timestamp `20260102-030405`.

| Name | Contents |
| --- | --- |
| `care-20260102-030405.dump.enc` | Scheduled PostgreSQL custom-format dump, encrypted as an OpenSSL CMS envelope. |
| `files-20260102-030405.tar.gz.enc` | Matching encrypted gzip-compressed tar archive of the storage volume. |
| `care-manual-20260102-030405.dump.enc` | Manual database dump. |
| `files-manual-20260102-030405.tar.gz.enc` | Matching manual files archive. Pairs only with the `care-manual-` dump of the same timestamp, never with a scheduled one. |
| `care-20260102-030405.dump` | Recognized unencrypted database dump, supported as restore input. Current backup writers do not create this final form. |
| `files-20260102-030405.tar.gz` | Recognized unencrypted files archive, also supported as restore input. |
| `backup-cert.pem` | Public certificate identifying the backup encryption key. It cannot decrypt backups. |
| `.backup.lock` | Lock file shared by scheduled and manual writers in this folder. Its existence does not itself mean a writer is running. |
| `.care-20260102-030405.dump.tmp` | Intermediate plaintext database dump during backup creation. |
| `.care-20260102-030405.dump.tmp.enc` | Intermediate encrypted dump, before publication under its final name. |
| `.files-20260102-030405.tar.gz.tmp` and its `.enc` counterpart | Equivalent intermediate files for the files archive. |

The timestamp format is `YYYYMMDD-HHMMSS`. The filenames contain no timezone.
Manual timestamps come from the Go process; scheduled timestamps come from the
sidecar's `date`. The list sorts by the timestamp embedded in the filename, not
by filesystem modification time.

The suffix `.enc` identifies encrypted backup content. The separate
`CARE-<clinic>-backup-recovery.pem` file is a private key, not a backup.

### Host folders versus container paths

`Store.Dir` is the installation directory. `Store.BackupDir` is the selected
backup destination. With no configured override, the default is
`care-db-backups` inside the Windows Desktop known folder on Windows, or
`<user home>/Desktop/care-db-backups` on other platforms, from
[`Clinic.backupDir`](../app/internal/clinic/clinic.go).
Windows respects OneDrive and administrator redirection; it does not assume
the visible Desktop is `C:\Users\<name>\Desktop`. A known-folder lookup failure
is logged before falling back to that legacy path. An explicitly configured
backup destination is never relocated by this lookup.
Use the application's reported effective path rather than assuming that the
executable directory or current shell directory contains the backups.

Windows folder selection and recovery save dialogs start at the visible Desktop.
Setup shows free space without a days/years-of-backups estimate, retains
insufficient-space checks, and offers **Open folder** for saved recovery files
and admin codes. Desktop may be cloud-synced: use a secure separate location for
recovery materials and preferably an external drive for backups. CARE warns
about possible OneDrive syncing, but does not detect or block every cloud folder.

```text
<installation>/
|-- backend.env
|-- backup-state/
|   `-- backup-status        # last scheduled run (or successful manual one): running, ok, or failed and why
|-- keys/
|   `-- backup-cert.pem
`-- restore-state.json       # exists while a restore needs recovery/finalization

<separate backup folder>/
|-- backup-cert.pem          # public ownership marker, cannot decrypt
|-- care-20260102-030405.dump.enc
|-- files-20260102-030405.tar.gz.enc
|-- care-manual-20260103-101112.dump.enc
`-- files-manual-20260103-101112.tar.gz.enc
```

The backup service's mounts in
[`docker-compose.yml`](../deployments/docker-compose.yml) provide:

| Container path | Purpose |
| --- | --- |
| `/backup.sh` | Read-only mounted backup script. |
| `/backups` | Writable backup destination supplied through `BACKUP_DIR`. |
| `/keys` | Read-only installation keys directory. The writer uses the certificate, not the private key or its password. |
| `/minio-data` | Read-only mount of the storage data volume for archiving. |
| `/state` | Writable `<installation>/backup-state`, holding `backup-status` from each scheduled run. |

The Compose file also has a raw relative backup-path fallback. Running Compose
outside the desktop's configured environment is not equivalent to using the
application's effective backup destination.

[`backup.Dockerfile`](../deployments/backup.Dockerfile) is deliberately small:
it inherits the pinned `POSTGRES_IMAGE` and adds OpenSSL with `apk`. It does not
contain a Go scheduler or bake the mounted script into the image. The runtime
image must also supply the PostgreSQL and shell utilities the script uses,
including `tar`, `find`, and `flock`; the Dockerfile explicitly adds only
OpenSSL.

## 3. Backup recovery file and Desktop admin recovery

### These are not interchangeable

| Item | What it does | What it cannot replace |
| --- | --- | --- |
| `keys/backup-cert.pem` | Public certificate for unattended encryption; also copied into the backup folder as an ownership marker. | The private recovery file. This is not the clinic's HTTPS certificate. |
| `CARE-<clinic>-backup-recovery.pem` | Private key exported to a location chosen by the clinic manager. Select it to restore encrypted backups. | Backup data, Desktop authorisation, or another clinic's key. |
| Desktop admin password | Authorises protected Desktop operations. | Backup decryption or the independently stored CARE web login. |
| Six Desktop admin recovery codes | Each unused code can reset the Desktop password offline. | Backup decryption or CARE web password recovery. |
| Database password | Authenticates PostgreSQL commands. | Any recovery material. |

### Setup and custody

There is no backup password or OS-keyring entry. During setup,
[`app_recovery.go`](../app/app_recovery.go) uses Go's standard cryptographic
library to generate a random RSA-4096 key and a self-signed X.509 certificate
with `CN=care-backup` and 100-year validity. The PEM private key is compatible
with OpenSSL. It is written only to the user-selected recovery file, never to
the installation or backup folder. The config retains only the public
certificate, the selected path and verification state.

The manager must select the saved file again. CARE parses it and matches its
public key to the clinic certificate before installation is allowed. Recovery
files must be outside the installation, settings, logs and backup directories;
the UI asks the manager to keep them outside the computer and retain a second
secure copy. CARE cannot prove the chosen storage is physically offsite.

Setup presents separate **Saved** and **Verified** completion cards, with
checkmarks and a clear pending action for each step. The six-code sheet has its
own saved confirmation; a cancelled or failed operation does not mark a step done.

**Anyone with the recovery file and the backups can decrypt patient data.**
Keep them separate. If all copies of the recovery file are lost, the encrypted
backups cannot be unlocked. The recovery file contains no patient-data backup.
Uninstall does not delete user-exported recovery materials.

`InstallCertificate` validates and installs only the public certificate.
Copying the same certificate is idempotent; a different certificate or a
symbolic link is not overwritten. Foreign backup folders are rejected.
`CopyBackupCertificate`, `PreserveBackupCertificate` and
`DiscardUnusedCertificate` manage this public ownership marker, not a secret.
`BackupEncryptionOn()` only checks certificate presence, not recoverability.

Exports are exclusive-create files with requested mode `0600`, synced before
success. Existing exports are never overwritten. Keys directories use `0700`.
POSIX modes are not equivalent to Windows ACL guarantees.

### Offline Desktop password recovery

Setup also exports a printable text sheet of exactly six random 128-bit codes,
including clinic name, issue time and usage instructions. The application
stores only SHA-256 hashes of the normalised codes; spaces, hyphens and case
do not affect entry. Plaintext codes are never returned through the UI bridge.

Advanced provides **Forgot Desktop password?**, **Change Desktop password** and
**Replace recovery codes**. A reset atomically changes the bcrypt password hash
and clears the used code hash. Other codes remain usable. Failed writes leave
both unchanged in memory. Five invalid attempts start a persisted cooldown;
further invalid attempts increase it, capped at eleven minutes.
Authenticated replacement saves a new sheet before activating its hashes and
invalidating the entire prior set. Cancelling the dialog changes nothing.

These operations do not require Docker or internet access. They do not change
the CARE web `admin` password, which setup only initialises to the same value.
Local OS-account access remains a separate security boundary: an OS user who
can rewrite the application's configuration is not constrained by this gate.

### Encryption and restore

OpenSSL CMS still encrypts each backup with AES-256-CBC in a streaming DER
envelope addressed to the public certificate. Restore explicitly mounts the
selected recovery file read-only into its preflight helper and decrypts there,
before stopping writers or replacing data. There is no local-key fallback.
Database credentials remain in process environment variables, not argument
values. Private-key contents are not placed in arguments, logs or the journal.

Encryption is not a signed manifest or proof of cross-file consistency.
Only restore backups from trusted sources. Docker administrators and privileged
OS users can inspect mounted secrets and decrypted staging data.

## 4. Creating backups and applying retention

### Manual backup

[`Clinic.BackupNow()`](../app/internal/clinic/backup.go):

1. Refuses to run if the certificate-presence check says encryption is absent.
2. Refuses to run if the backup folder's drive has less free space than a
   full set needs (see below), naming both sizes.
3. Generates a timestamp in Go.
4. Runs `sh /backup.sh once manual-<timestamp>` using `docker compose exec -T`
   in the already running `backup` service.
5. Logs the resulting `care-manual-<timestamp>.dump.enc` and
   `files-manual-<timestamp>.tar.gz.enc` paths.

The script's `once` mode refuses to start if either final name already exists,
then writes the dump and the files archive exactly as the scheduled cycle does.
If the files step fails, the published dump stays and is listed as
database-only, but the command exits non-zero so the app reports the failure.
It does not apply retention or write the status file, and it does not start a
stopped clinic. An execution
failure re-checks free space first, so a full drive is reported as such; any
other failure reports that CARE must be running to take the backup.

### Space checks and the status file

A backup run briefly holds more than its final size: each member is written as
plaintext, sealed next to it, and only then is the plaintext removed. With `d`
the dump size and `f` the files archive size of the newest daily set, the peak
is `max(2d, d + 2f)`. The required space is that peak plus a quarter for growth
plus 256 MB, and never less than 1 GB. Manual and scheduled runs use the same full-set estimate.
With no earlier set, the script estimates from `pg_database_size` and
`du -sk /minio-data`; the app uses the 1 GB floor.

The same formula lives in two places that must agree:
`storage.BackupNeed` in Go and `need_kb` in `backup.sh`.

| Where | Check | On failure |
| --- | --- | --- |
| Choosing a folder (setup and Backups tab) | `ValidateBackupDir` compares free space on the chosen drive with the need sized from the *current* folder's backups. | The folder is refused with both sizes. A folder on the clinic data drive is accepted, with a note recommending a USB drive. |
| Every scheduled run | `check_space` before anything is written. | Nothing is written; `[backup] ERROR: not enough space in the backup folder`; retention skipped. |
| Any step failing mid-run | `disk_full` re-reads `df`: under 256 MB free, or under the need, is classified as a full drive. | Reason `disk_full` instead of `error`. |

Scheduled runs write `backup-state/backup-status` under the install folder
(mounted at `/state`), never in the backup folder, which may be the full drive.
A successful **Back up now** also writes `state=ok`, so it clears a failed daily
run as the failure message suggests; a failed or refused manual backup leaves the
file alone, so it never hides or overwrites the daily run's state:

```text
state=failed        # running | ok | failed
reason=disk_full    # disk_full | error | empty
at=1767225600
need_kb=1048576
free_kb=1024
message=not enough space in the backup folder
```

The app reads it on each storage check. `failed` raises the red panel banner
and an alert on the Backups tab with the sizes and a "Choose another folder"
button. Independently, if the clinic is running, no run is in progress, and the
newest backup file is more than 26 hours old, the report is marked `stale`,
which catches a sidecar that is not running at all. A `running` state older than
6 hours is treated as abandoned so a killed container cannot hide staleness.

With retention `0` (keep forever) the Backups tab also shows roughly how many
days of backups still fit: free space minus the need, divided by the newest set's
size. Under 30 days is shown as `low`.

Manual and scheduled backup creation share the same script implementation,
including its certificate/database-password guards and locking. There is no
second inline dump/encryption implementation in the Wails backend.

### The schedule lives in the sidecar

In its normal mode, the script attempts a complete set immediately, then sleeps
for `86400` seconds after the attempt. It repeats while the container runs.

This is not a midnight cron job, a Wails timer, a missed-run catch-up mechanism,
or a cloud backup service. Cycle duration is additional to the sleep interval.
After a failed cycle it logs the failure and still sleeps before its next
attempt.

The script refuses to start without `/keys/backup-cert.pem` or a nonempty
`POSTGRES_PASSWORD`. It defaults other database connection values to host `db`,
port `5432`, user `postgres`, and database `care`.

Both scheduled and manual backups wait for PostgreSQL to accept connections
before dumping. Docker can restart the sidecar before the database after a
runtime reboot, without reapplying Compose's initial `depends_on` ordering.
Readiness uses the configured database connection values, with at most 30
`pg_isready` checks, each bounded to five seconds and separated by five seconds.
A scheduled attempt remains `running` while waiting. If the database never
becomes ready, it records `database_unavailable`, skips all dump/archive and
retention work, and reports the database problem rather than blaming the backup
folder. Existing backup files remain untouched. A later successful scheduled or
manual backup clears the failure status.

### Locking and publication

```mermaid
flowchart TD
    Request["Scheduled cycle or manual once"] --> Lock["Acquire exclusive flock on .backup.lock"]
    Lock --> Collision["Reject existing final paths, including dangling links"]
    Collision --> Ready["Wait for PostgreSQL to accept connections"]
    Ready --> Dump["pg_dump -Fc into hidden plaintext file"]
    Dump --> Verify["pg_restore --list"]
    Verify --> Encrypt["CMS encrypt into hidden encrypted file"]
    Encrypt --> Remove["Remove plaintext; fail if removal fails"]
    Remove --> Publish["Rename encrypted file to final dump name"]
    Publish --> Files["Archive and verify storage files; encrypt; remove plaintext; rename"]
    Files --> Mode{"Scheduled?"}
    Mode -- No --> Unlock["Release lock and return"]
    Mode -- Yes --> Prune["Remove abandoned intermediates and apply retention"]
    Prune --> Result["Report success only if all required steps succeeded"]
    Result --> Sleep["Release lock, then sleep 86400 seconds"]
```

`flock -x` on file descriptor 9 serializes cooperating manual and scheduled
writers in the same backup folder. A waiting manual backup does not prune an
active scheduled backup's intermediate files. The scheduled loop releases the
lock before sleeping. The lock is advisory: unrelated programs that ignore it
can still modify the folder.

Before a scheduled or manual set starts, **both** final output paths must be unused. Existing files, directories, and even
dangling symbolic links at those final names cause refusal rather than
intentional replacement.

Each individual file follows this order:

1. Write plaintext under a hidden intermediate name.
2. Verify its format.
3. Encrypt to another intermediate name.
4. Remove plaintext and check that removal succeeded.
5. Rename the encrypted intermediate file to its final name in the same folder.

For the database, capture-time verification is `pg_restore --list`. For files,
`tar -czf` must succeed, the archive must be nonempty, and `tar -tzf` must
succeed. A failed archive operation is not excused merely because it left a
readable-looking output file.

The files command archives `.` from the storage volume root, including its
hidden contents. It is a filesystem archive of that volume's layout, not an
object-by-object export through the storage API.

The final-name rename gives **per-file atomic publication** under normal
same-filesystem rename semantics. It does not atomically publish the two files
as one set. A database dump may remain published if the later files step fails.
The script also does not explicitly fsync its published backup files and
directory as the restore journal writer does. Do not turn atomic publication
into a blanket power-loss durability claim.

### Retention `0` really means keep published backups

The script reads `DB_BACKUP_RETENTION_PERIOD`, defaulting an absent/empty value
to `0`. The settings editor's validation and persistence of the literal zero
are described in [configuration and settings](configuration-and-settings.md).

After both members of a scheduled set publish, `prune()`:

1. Deletes top-level **regular files** matching `.care-*.tmp*` or
   `.files-*.tmp*`.
2. With retention `0`, keeps published backups and returns.
3. Otherwise deletes top-level regular files matching `care-*.dump*` or
   `files-*.tar.gz*` using `find -mtime +<days>`.

Consequences:

- Zero does not mean "disable backups" or "delete everything".
- Zero still permits removal of abandoned intermediate files after a successful
  scheduled set.
- Age is based on filesystem modification time and `find`'s whole-day
  calculation, not the filename's timestamp or a count of retained sets.
- Manual dumps and manual files archives match the retention globs too,
  although manual mode itself never calls `prune()`.
- Directories, nested contents, and symbolic links are not recursively pruned.
- The recovery key and `.backup.lock` are not retention targets.
- The shell globs are broader than the Go restore filename expressions. Keep
  unrelated files with backup-like names out of this directory.

The log says it is deleting old "sets", but deletion is actually file-by-file.
There is no database/files transaction for pruning.

### Failures are not all equivalent

| Failure point | What can already exist | Reported behavior |
| --- | --- | --- |
| Certificate/password guard or lock acquisition | Existing backups remain; no new set is intentionally written. | Refuse startup/attempt. |
| Not enough free space | Nothing new. | Refuse before writing; status `failed` / `disk_full`; retention skipped. |
| Final-name collision | The colliding entry is retained. | Refuse overwrite before the relevant work; scheduled mode checks both names before dumping. |
| Dump, verification, encryption, or publication | An intermediate file can remain if cleanup also fails. | Fail the step; skip retention. |
| Files step after dump publication | A new database-only dump may already be visible. | Fail the set; skip retention, rather than claiming a complete set. |
| Plaintext removal | Plaintext may remain in the folder. | Refuse to publish that member as though cleanup succeeded. |
| Temporary-file or retention pruning | Both new members are published; some cleanup deletions may already have happened. | Report "published, but cleanup FAILED", not success and not "every old backup retained". |

An interrupted process or failed deletion can therefore leave plaintext
intermediates. A later successful scheduled cycle attempts abandoned-file
cleanup, but that is not a guarantee of immediate removal or secure erasure.

## 5. Discovering and selecting a restore

### Desktop backup summaries and restore consent

Overview presents the newest backup's status, size and encryption without
printing its filesystem location. Backups retains the effective destination
and folder-management controls; hiding the summary path does not move files or
change the backup policy.

The policy card calls `GetBackupPolicy()` for the 86,400-second interval and
installed retention value. Missing or invalid settings produce a retryable
policy error, not a hard-coded default. The interval is not a fixed nightly
time. Current backup activity and completion come from native state and events;
request acceptance alone cannot mark a backup successful.

There is one visible **Restore from a backup file** route for local and imported
files. It calls `ChooseBackupFile`, then `InspectBackupFile`, and presents the
database-only or database-plus-files scope. Before `RestoreFromFile` it
re-inspects the selected metadata, checks the Desktop password, requires the
selected recovery file for encryption and requires explicit replacement
acknowledgement. The protected native job performs the deeper checks described
below; the UI does not replace them.

Cancel and Forgot Desktop password remain usable before native submission,
including while a read-only picker or preflight result is pending. Cancellation
invalidates that operation, clears the password and acknowledgement, and keeps
the last accepted file selection. A late picker cannot replace it or reopen the
dialog. After submission, cancellation is disabled because the native job may
already be modifying data.

Desktop updates block backup mutations with both rendered disabled controls
and a live guard. Update progress invalidates earlier folder picks, backup and
recovery picks, and restore preflight. Even if the installer handoff is
acknowledged before an old promise returns, that old work cannot write a new
destination or start a restore. The user must repeat the interrupted action.

See [`backups-tab.tsx`](../app/frontend/src/screens/panel/backups-tab.tsx),
[`restore-backup-dialog.tsx`](../app/frontend/src/screens/panel/restore-backup-dialog.tsx),
and their [UI regressions](../app/frontend/tests/backups.spec.ts).

### Native backup inventory

[`ListBackups()`](../app/internal/backup/restore.go) reads only the configured
backup folder. A missing folder returns no entries without an error.

It recognizes regular dump files with valid timestamps and constructs one
`Backup` value per dump:

| Field / JSON name | Meaning |
| --- | --- |
| `DBDump` / `db_dump` | Dump basename, not an arbitrary path. |
| `FilesArchive` / `files_archive` | Selected matching archive basename, or empty for database-only. |
| `Label` / `label` | Human-readable time, `daily` or `manual`, `DB only` or `DB + files`, and an encryption suffix when applicable. |
| `Manual` / `manual` | Whether the dump name starts with `care-manual-`. |
| `Encrypted` / `encrypted` | Whether the dump **or its selected archive** has `.enc`. |
| `SizeBytes` / `size_bytes` | Size of the dump alone, not the combined set size. |

Pairing uses the set name: `care-<ts>` pairs with `files-<ts>` and
`care-manual-<ts>` with `files-manual-<ts>`. A manual dump is never paired with a
scheduled archive of the same timestamp. It prefers an archive with the same
encrypted/plain form as the dump, then accepts the other form if that is the
available match. Orphan files
archives do not create list entries.

The label `daily` comes from the filename convention; it is not proof that a
scheduled cycle succeeded. Listing is discovery, not decryption or a successful
restore rehearsal. It does not fully inspect dump/archive contents, and the
stricter restore path rejects empty files even if their names appeared in a
list.

### Configured folder and external import use the same pipeline

```mermaid
flowchart TD
    Local["Restore: configured backup folder"] --> Common["RestoreFrom"]
    External["External selection: source folder plus basenames"] --> Common
    Common --> Names["Validate names, timestamps, regular files, and scope"]
    Names --> Enc{"Any selected member encrypted?"}
    Enc -- No --> Pending["Reject an existing or invalid recovery journal"]
    Enc -- Yes --> Key["Require explicitly selected backup recovery file"]
    Key --> Pending
    Pending --> Images["Require staged migration wiring and restore images"]
    Images --> Journal["Create staging journal"]
    Journal --> Stage["Copy into owned staging volume and validate"]
```

- `Restore(dbDump, filesArchive, recoveryFile)` delegates to `RestoreFrom` using
  `Store.BackupDir`.
- `RestoreFrom(srcDir, dbDump, filesArchive, recoveryFile)` uses the supplied
  folder, or the configured folder when `srcDir` is empty, and converts it to
  an absolute path.
- The dump and optional archive must be in that same source folder, with the
  required basenames. This is not a remote URL fetch or a files-only restore.
- An encrypted member requires a selected, valid private recovery file.
- There is no automatic lookup beside backups or fallback to an installation key.
- Neither source-folder restore nor staging relocates the original backups into
  the configured destination. The input is mounted read-only and copied into
  a Docker staging volume for this operation.

The desktop pickers, job handling, and administrator authorisation belong to the
[Wails application guide](wails-application.md). At the package boundary, import
is simply a different source folder, not a separate destructive restore
algorithm.

### Filename and filesystem checks

Restore refuses:

- A dump name outside the `care-[manual-]YYYYMMDD-HHMMSS.dump[.enc]` convention.
- A calendar-invalid timestamp.
- A files archive from another set: a different timestamp, or a scheduled
  archive with a manual dump and the other way round.
- Path separators, traversal, or arbitrary shell text in the basenames.
- A selected backup that `Lstat` identifies as a directory, symbolic link,
  non-regular file, or empty file.
- A chosen recovery key that is not a nonempty regular file.

During the container copy step, each source backup is checked again with
`-f` and `! -L`. Docker `--mount` arguments are assembled using CSV escaping,
so a comma in a host path is not simply concatenated as a new mount option.

These are deliberate entry/path checks, not a claim to eliminate every
filesystem race. The code does not freeze an externally writable source folder
or forbid all symlinked ancestor directories. Use stable, trusted source
folders.

## 6. Preparing the replacement without replacing live data

### The `Store` and callback boundary

[`Store`](../app/internal/backup/store.go) is a dependency container, not an
independent running service:

| Field or constructor | Contract |
| --- | --- |
| `Dir`, `BackupDir` | Installation and backup folders. The journal and connection settings are installation-local. |
| `Image`, `BackendImage` | Backup-tools image and CARE backend image. The backend image supplies Python for archive validation and is needed for staged migrations. |
| `Host` | Clinic hostname used in the completion message; not the PostgreSQL host. |
| `Project` | Docker project/network/resource prefix; the clinic wiring uses `care-desktop`. |
| `Log` | Optional log callback. |
| `EnsureImage` | Callback to ensure the backup image, used by key setup and prepared recovery. |
| `EnsureRestoreImages` | Callback ensuring the images needed for staged restore. |
| `Migrate(database, restoreID)` | Callback that must migrate the named staging database, not the current live database. |
| `New(run, s)` | Attach the supplied `proc.Runner` to the supplied store and return that same store. It does not create keys or launch work. |

[`Clinic.Backups()`](../app/internal/clinic/backupstore.go) fills these fields
and attaches the clinic runner, which supplies the working directory,
environment, and command logging. It wires:

- `EnsureImage` to `Builder().EnsureBackupImage`.
- `EnsureRestoreImages` to ensuring the backend image, then the backup image.
- `Migrate` to `Clinic.migrateStaged`.

Restore explicitly refuses missing `EnsureRestoreImages`, missing `Migrate`, or
an empty backend image before creating its journal. Key generation also
expects a properly wired `EnsureImage`. Constructing a bare `Store` is not a
substitute for these contracts.

### Resource identities

Each restore gets a random 12-byte identifier rendered as 24 lowercase
hexadecimal characters. For a synthetic ID such as
`0123456789abcdef01234567`:

| Resource | Naming / ownership |
| --- | --- |
| Staged database | `care_restore_<id>` |
| Previous database | `care_previous_<id>` |
| Staging Docker volume | `<project>_restore_<id>_stage` |
| Previous-files Docker volume | `<project>_restore_<id>_previous` |
| Helper container | `<project>-restore-<id>-<step>` |
| Restore label | Exported `RestoreLabel` constant: `org.care-desktop.restore=<id>` |
| Project label | `com.docker.compose.project=<project>` |
| PostgreSQL ownership/application tag | `<project>:restore:<id>` |

The stage volume exists for database-only restores too. It holds copied dump
input and the decrypted `database.dump`; a DB + files restore also holds
`files.tar.gz` and its extracted `files/` directory. For encrypted inputs, the
copied encrypted inputs remain there as well.

The previous-files volume is only needed when files are being restored. The
previous database is preserved by renaming, not by making a second database
dump at cutover.

### Preflight is more than checking an archive's table of contents

The preparation order in
[`prepareRestore`](../app/internal/backup/restore.go) is:

1. Create and verify ownership of the staging volume.
2. Run a backup-image helper with **no network**, read-only source/key mounts,
   a writable stage volume, and `umask 077`.
3. Copy the selected inputs into staging. Decrypt encrypted members there,
   using the selected recovery file, mounted read-only, with no password.
4. Fully read the dump with
   `pg_restore --exit-on-error --file=/dev/null /restore/database.dump`,
   then sync.
5. If files were requested, validate and extract the archive into staging.
6. Start/wait for only the database service without recreating it:
   Compose uses `--no-recreate`, `--wait`, and a 300-second wait timeout.
   A separate `pg_isready` command must also succeed.
7. Inspect the existing database identities, record the original database's
   OID in the journal, and require that no staged/previous name is already in
   use.
8. Create the tagged staging database from `template0` and load the dump into
   it with `--exit-on-error --single-transaction --no-owner --no-privileges`.
9. Run migrations against that staging database.
10. Recheck that the live database still has its original OID, the staged
    database has the correct restore tag, and the previous name is still
    unused. Record the staged database's OID.

An **OID** is PostgreSQL's object identifier. Renaming a database changes its
name, not its OID, so recovery can distinguish the original from the replacement
even when one of them now uses the ordinary live name.

Capture-time `pg_restore --list` can succeed for a dump whose later payload is
corrupt. Restore's full read catches more than that listing, and the actual
transactional load checks whether the SQL can be applied to the staging
database. These are separate checks, neither of which proves clinical data
correctness.

`--single-transaction` applies to loading the dump. It is not a transaction
covering database creation, all migrations, files, and activation together.
If the load fails, an empty tagged staging database can still need cleanup.

### Archive validation and extraction safety

[`validateRestoreFiles`](../app/internal/backup/restore_data.go) runs Python
from the backend image with:

- `--network none`.
- A read-only container root filesystem.
- A writable staging-volume mount.
- Explicit `--entrypoint python`, `-B`, and user `0:0`.

It does not boot the backend application to extract an archive.

The Python code first drains the gzip stream completely, then reads the tar
members. Before extracting any member, it requires every entry to:

- Have a nonempty, relative POSIX path.
- Contain no `..` component.
- Be a regular file or directory.

Symbolic links, hard links, devices, FIFOs, and other non-file/non-directory
entries are rejected. Only after checking the member list does it create the
staging `files/` directory, extract with numeric ownership, and sync.

This protects the live volume from malformed or path-escaping archive contents.
It does not impose an archive expansion limit, member-count limit, or free-space
budget. It also does not make untrusted database SQL safe. Disk exhaustion,
incompatible data, and malicious content remain reasons to use trusted backups
and an isolated recovery exercise.

### Why staged migrations are not live migrations

The callback receives both `care_restore_<id>` and the restore ID.
[`Clinic.migrateStaged`](../app/internal/clinic/migrate.go) validates that
relationship, then runs a one-off backend service command:

```text
docker compose run --rm --no-deps
    ... --entrypoint python
    -e POSTGRES_DB -e DATABASE_URL -e PGAPPNAME
    backend manage.py migrate --noinput
```

This is a schematic excerpt, not a command to run against a clinic.

The callback overrides **both** `POSTGRES_DB` and `DATABASE_URL`. Merely changing
one would be insufficient if the backend preferred the other. Its URL helper
changes the database path and removes competing `dbname`/`database` query
parameters. The restore tag is placed in `PGAPPNAME` and the URL's
`application_name` so interrupted database sessions can be identified.

`--no-deps` prevents this helper from starting ordinary dependencies as a side
effect, and the Python entrypoint bypasses the backend's usual service startup.
During this preparation, existing CARE services have not yet been stopped.
The intended database target is the isolated replacement, not the live schema
used by current workers.

That is why this hook must not be replaced with the normal startup/live
migration routine. The [clinic lifecycle guide](clinic-lifecycle.md) separately
explains worker pausing around ordinary live migrations.

The restore code's SQL helpers use the `POSTGRES_*` connection settings in
`backend.env`. A configured `DATABASE_URL` used for migrations still needs to
describe the intended PostgreSQL server. The source does not prove arbitrary,
conflicting connection configurations equivalent, nor can it guarantee that
every upstream/plugin migration lacks external side effects.

## 7. Cutover, durable state, and activation

### Stopping writers is a checked operation

After successful staging and migrations, restore stops these Compose services:

```text
backend  celery-worker  celery-beat  minio  backup  caddy
```

It then obtains their container IDs and inspects each status. Every returned
container must be `exited` or `created`; a failed stop, incomplete inspection,
or still-running service prevents cutover.

This stops normal application, worker, scheduler, upload, backup, and ingress
activity before replacing data. It is not a universal lock on unrelated
PostgreSQL clients or administrator commands. It also means restore is **not
zero-downtime**. Even a database-only restore stops this service list, although
it does not replace the uploaded-files volume's contents.

For a DB + files restore, the stopped live files are copied with `cp -a` into
the owned previous-files volume and synced before the cutover phase is
recorded. During this initial snapshot, the code can create the normally
labelled live volume if it is absent. That can represent an empty source;
it is not proof that missing historical uploads have been recovered.
Later prepared recovery requires the existing recovery/live volumes and
does not manufacture an empty replacement for a missing recovery copy.

### The journal is local and durable by design

[`restore_journal.go`](../app/internal/backup/restore_journal.go) records
`<installation>/restore-state.json`. Its JSON fields are:

| Field | Purpose |
| --- | --- |
| `id`, `project` | Identify this restore and this installation's resource namespace. |
| `phase` | One of the four exact states below. |
| `database`, `host`, `port`, `user` | Original database connection identity. These are distinct from the clinic's web hostname. |
| `original_oid`, `staged_oid` | Original/replacement identities, populated as staging progresses. |
| `with_files` | Whether uploaded files participate in cutover/recovery. |

It does not store the database password, private-key contents,
source folder, or selected backup filenames. Passwords are not needed to replay
rollback from the already prepared resources.

The journal reader requires a regular non-symlink file no larger than 16 KiB,
valid JSON, a valid restore/project identity matching the current store, valid
connection fields, and recognized phases. Prepared/committed journals require
two distinct valid OIDs. Protected PostgreSQL database names such as `postgres`,
`template0`, and `template1` are rejected.

Writes use
[`atomicfile.Write`](../app/internal/sys/atomicfile/atomicfile.go) with mode
`0600`: write a sibling file, sync and close it, then replace the journal.
Unix replacement syncs the containing directory; Windows uses replacement
with write-through semantics. This is the durability mechanism used to order
recovery decisions. It is not a promise that a failed disk, filesystem, Docker
engine, or power-loss scenario has been exhaustively tested.

Before each SQL helper, current database name/host/port/user settings are
compared with the journal. A mismatch stops recovery with a request to restore
the original connection settings. The database password is reread from
`backend.env`; it is not frozen in the journal.

### The four exact phases

| On-disk phase | When it is written | What the next recovery does |
| --- | --- | --- |
| `staging` | Before helper resources are created; rewritten as original/staged OIDs become known. | Stop this restore's helper containers, leave live data alone, then mark `rolled-back`. Staging is abandoned, not automatically resumed. |
| `prepared` | After replacement validation/migrations, checked writer shutdown, and the previous-files snapshot when applicable. Before modifying live files or renaming databases. | Stop/verify writers and helpers, check ownership, restore the original database/files, then mark `rolled-back`. |
| `committed` | After files replacement when applicable and a successful database swap, **before** starting the normal stack. | Keep the replacement as live. Never automatically replay the old snapshot over potentially newer writes. |
| `rolled-back` | After staging abandonment or successful prepared rollback. | Do not replay rollback again. Keep current data and leave final cleanup for a successful start. |

There is no separate persisted `activating`, `healthy`, or `finished` phase.
A removed journal means no restore is pending in this protocol; malformed
metadata is an error, not permission to treat it as absent.

```mermaid
stateDiagram-v2
    [*] --> staging: Inputs and image preparation accepted; write journal
    staging --> prepared: Stage and migrate; stop writers; preserve files if needed
    staging --> rolled_back: Recover by abandoning staging
    prepared --> committed: Replace files; swap databases; persist commitment
    prepared --> rolled_back: Recover original data before activation
    committed --> committed: Recover without reverting new writes
    rolled_back --> rolled_back: Recover without repeating rollback
    committed --> [*]: Successful activation then FinishRestore
    rolled_back --> [*]: Successful normal start then FinishRestore
    state "rolled-back" as rolled_back
```

### Database and file cutover are different mechanisms

Files are replaced **in the existing live volume**:

1. Verify the source staging volume's labels and the live volume's Compose
   project/volume labels.
2. Remove current top-level entries, including hidden entries.
3. Copy the staged `files/` tree into the live volume.
4. Sync the live volume.

This is a stopped-writer, journal-recoverable copy, not an atomic volume rename.
Interruption can leave the live files partly replaced; the separately retained
previous-files volume is what makes prepared recovery possible.

The database swap uses both Go-side inspection and SQL assertions:

1. Require the live OID to be the original, the staged OID/tag to be the
   replacement, and the previous name to be unused.
2. Disable new connections to the live and staged databases.
3. Terminate their sessions and assert no sessions remain.
4. In one SQL transaction, rename live to `care_previous_<id>` and staged to
   the original live name.
5. Re-enable connections to the newly live database.

The two renames are transactional; the entire restore is not. In particular,
connection disabling occurs outside that rename transaction. If interruption
leaves the original database in place but connections disabled, recovery
explicitly reconnects it.

```mermaid
sequenceDiagram
    participant S as backup.Store
    participant J as Local journal
    participant H as Staging helpers and migration callback
    participant W as CARE writers
    participant D as PostgreSQL and files volumes
    S->>J: Persist staging
    S->>H: Copy, decrypt, fully validate, load separate DB, migrate it
    H-->>S: Replacement ready; original and staged identities checked
    S->>W: Stop and inspect all listed writers
    S->>D: Preserve previous files when included
    S->>J: Persist prepared
    S->>D: Replace live files when included
    S->>D: Transactionally swap database names
    S->>J: Persist committed
    S->>W: Compose up with wait
    S->>S: Wait for CARE health
    S->>D: FinishRestore removes owned previous/staging resources
    S->>J: Remove journal last
```

After committing, restore calls Compose `up -d --wait --wait-timeout 300`,
then `health.Wait` with a three-minute timeout. Only after these checks does
this path call `FinishRestore()` and log completion.

## 8. Recovery after interruption

### Start through the application

Errors after journal creation generally identify the restore and say that
recovery data was retained. They direct the operator to start CARE to recover
safely and not start containers manually.

That instruction matters:
[`Clinic.Start`](../app/internal/clinic/start.go) calls `RecoverRestore()`
before ordinary startup and `FinishRestore()` after successful health.
Starting writers outside that lifecycle can introduce new data before the
recorded recovery decision has been applied.

Keep the installation's journal and the owned staging/previous Docker resources
intact while investigating. Do not "fix" an error by deleting
`restore-state.json`, editing OIDs/phases, renaming databases, or replacing a
missing recovery volume with an empty one.

### Recovery decisions by interruption point

| Interruption or error | Durable phase normally left behind | Intended next action |
| --- | --- | --- |
| Input rejection or image preparation failure before the journal | None | Report the error; no restore resources have been intentionally created by this pipeline. |
| Copy, decrypt, full dump read, archive validation, staged load, or staged migration | `staging` | Abandon staging on recovery; originals were not intentionally replaced. |
| Writer stop/inspection or previous-files snapshot failure | `staging` | Preserve originals, but recognize that services may already be stopped. Recover and use normal startup. |
| After preparation, during files replacement, before/during/after database swap but before commitment | `prepared` | Roll back to the original database/files before normal writers start. |
| After commitment but before or during Compose activation | `committed` | Resume with the replacement. Some services may already have accepted writes. Do not automatically roll back. |
| Health failure after activation | `committed` | Preserve current replacement data and retained originals; retry normal startup/recovery, then finalize only after success. |
| Cleanup failure after successful start | `committed` or `rolled-back` | Retry finalization; some owned cleanup may already have completed. |

An error message alone does not establish which metadata write reached storage.
The validated journal read on the next attempt is the recovery authority.

### What `RecoverRestore()` actually does

With no journal it returns without starting Docker work. With a journal it
first finds only containers bearing **both** this project's label and this
restore's label, force-removes those helpers, and rechecks that none remain.
This also covers an interrupted migration helper.

For `prepared` recovery it additionally:

1. Stops and verifies the normal writers again.
2. Ensures the backup image when the callback is supplied.
3. For a files restore, requires the labelled previous-files volume and the
   correct existing live volume before attempting database recovery.
4. Starts/checks PostgreSQL and terminates remaining database sessions tagged
   with this restore's application name.
5. Inspects the database identities.
6. Either reconnects the still-original database, or transactionally renames
   the original back to live and the replacement back to staged.
7. Copies the previous files back into the live files volume when applicable.
8. Persists `rolled-back`.

Only two database arrangements are accepted for prepared rollback:

| Arrangement | Required identities | Recovery |
| --- | --- | --- |
| Swap did not happen | Live = original OID; staged = recorded replacement OID with this tag; no previous database. | Re-enable original connections. |
| Swap happened | Live = replacement OID with this tag; previous = original OID; no staged database. | Reverse the database-name swap and reconnect the original. |

Anything else is an error rather than a guessed rename. Missing/unowned
recovery volumes, an unremovable helper, changed settings, and unexpected OIDs
likewise stop the operation and retain metadata.

For `staging`, no files/database rollback is necessary: normal preparation
did not intentionally replace them. After stopping helper containers, recovery
marks the operation `rolled-back`; temporary resources remain for later
cleanup.

For `committed` and already `rolled-back`, recovery does not replay a
snapshot. This is essential: data written after an earlier successful
activation or rollback must not be overwritten on every later start.

`RecoverRestore()` by itself does not activate the whole clinic or declare it
healthy. It establishes which data normal startup may use.

### Why originals remain until `FinishRestore()`

[`FinishRestore()`](../app/internal/backup/restore_journal.go) only accepts
`committed` or `rolled-back`. It:

1. Stops lingering owned helper containers and tagged database sessions.
2. Rechecks live/previous/staged identities and staged ownership.
3. For a committed restore, drops the previous database if still present and
   still the recorded original.
4. For a rolled-back restore, drops the tagged staged database if present.
5. Removes the owned stage volume, plus the previous-files volume for a
   files restore.
6. Removes the journal **last**.

The normal live files volume is not a cleanup target. Previously published
backup files and recovery-key exports are not deleted by `FinishRestore`.

Missing resources that were already cleaned up can be accepted, allowing
partial cleanup to resume. A resource that exists under an unexpected identity
or label is not blindly removed. Cleanup is not one all-or-nothing transaction,
so a failure can leave only some of the recovery copies.

**Caller contract:** `FinishRestore()` does not run its own health check.
The restore and clinic-start callers arrange successful startup/health before
calling it. Calling this public method directly is not a safe health test and
would bypass that sequencing.

Retained originals are temporary recovery resources, not a user-selectable
second backup history. They occupy disk space until finalization succeeds.
After commitment, automatic recovery intentionally will not choose them over
possibly newer replacement data.

### Other public recovery operations

| Operation | Contract |
| --- | --- |
| `PendingRestore()` | Return whether a valid journal exists. Propagate unreadable/corrupt/foreign metadata as an error; do not interpret an error as "safe to start another restore". |
| `RecoverRestore()` | Resolve an interrupted restore before ordinary writers start; retain copies for successful-start finalization. |
| `FinishRestore()` | Finalize only a committed/rolled-back restore after the caller has established successful startup. |
| `Clinic.RestartBackupSidecar()` | Run recovery first, then force-recreate only the `backup` service. It is not whole-clinic activation or finalization. |
| `Clinic.BackupDirPath()` | Return the effective backup path; no disk backup is created. |

Starting another restore while any valid journal remains is refused, including
a committed restore whose cleanup still needs finishing.

## 9. Moving folders, preserving keys, and deleting backups

### Moving a folder does not recreate a key

Keep the private recovery file secure and separate when moving backups to
another folder or computer. Select the original clinic's recovery file for
imported backups. An encrypted dump and a public certificate cannot recover
data when every copy of the matching private recovery file has been lost.

External restore requires the original clinic's explicitly selected recovery
file, even when the current installation uses a different encryption key.
No cloud account or OS keyring sync is implemented here.

When the desktop changes its configured destination, it copies the public certificate
before using the new destination and recreates a running backup sidecar to
update mounts. Existing backups and their public certificate remain in the old folder; it is
not an automatic migration of old archives. The settings guide describes that
operation's configuration-save and rollback ordering.

### Key/location operations

| Operation | Contract |
| --- | --- |
| `EnsureKeysDir()` | Create the installation keys directory. It does not establish a valid keypair. |
| `GenerateRecoveryFile()` / `InstallCertificate(certificate)` | Generate a public certificate and private recovery file for export; install only the certificate without replacing a different identity. |
| `BackupEncryptionOn()` | Certificate-path presence check only. |
| `CopyBackupCertificate()` | Copy the public certificate to the backup destination without overwriting a different certificate. |
| `CheckLocation(dir, protected)` | Require absolute paths, resolve existing symlink ancestors even for not-yet-created descendants, and reject a backup directory equal to or inside the supplied protected directory. |
| `PreserveBackupCertificate()` | Check directory separation and retain the public ownership certificate before removing installed files. |
| `ForeignRecoveryData()` | Detect encrypted backups without a matching public certificate, or a certificate mismatch. This does not decrypt backups. |
| `DiscardUnusedCertificate()` | Remove a matching public certificate copy during failed-setup cleanup only when no encrypted backups exist. |
| `DeleteBackups()` | Explicitly delete recognized top-level backup-owned entries from the configured folder, keeping unrelated entries. |

The location check is about the protected directory supplied by its caller.
It is not a universal check of disk health, writability, every symlink race,
or every possible cleanup target.

Normal uninstall and purge preserve recovery keys when keeping backups.
The `RemoveUnusedRecoveryKey` option in
[`uninstall.go`](../app/internal/clinic/uninstall.go) exists for **failed-install
cleanup**, not routine uninstall/purge. Do not apply its narrow "provably unused
and ours" exception to keys protecting retained backups. Full lifecycle choices
belong in [cleanup and uninstall](cleanup-and-uninstall.md).

### Deletion has a different policy from retention

`DeleteBackups()` enumerates the selected folder's immediate entries:

- It recognizes the recovery key, `.backup.lock`, supported dump/archive
  basenames, and corresponding intermediate names after removing an initial
  dot and one `.tmp` component.
- It never recursively deletes directories.
- It leaves unrecognized entries in place and logs that they were kept.
- It removes the containing folder only when no unrelated entries were kept
  and the recognized removals succeeded.
- It aggregates removal errors; successful earlier deletions are not rolled
  back.

A recognized symbolic link can be unlinked by explicit deletion, but its
target is not recursively deleted. This differs from scheduled retention's
`find -type f` behavior, which excludes symbolic links.

Ownership here is a naming convention within the selected backup folder, not
proof of who created the data. A personal file deliberately given a recognized
backup name can be a deletion target. The exported recovery key is intentionally
a target when backups are explicitly removed.

This method does not acquire the shell's `flock`, stop services, or perform
authorization on its own. Those are caller/lifecycle responsibilities. Do not
invoke it concurrently with an independently running writer or treat it as
secure disk erasure.

## 10. Limits and operational expectations

The code supports guarded backup creation and staged restore, but several
boundaries are important:

1. **A local backup is still local.** This subsystem does not upload offsite,
   replicate to cloud storage, or protect against losing both the machine and
   its backup drive.
2. **A scheduled pair is not an application-wide snapshot.** PostgreSQL is
   dumped, then the storage volume is archived while ordinary services can
   still run. No cross-database/files snapshot transaction or quiescing step
   is implemented during backup capture.
3. **Listing and encryption are not restore verification.** Names, `.enc`,
   format checks, and a successful scheduled log do not prove usable clinical
   data on a replacement machine.
4. **Restore requires working infrastructure.** Docker/Compose, compatible
   images, required command-line tools, readable connection settings, and
   sufficient privileges must be available. Prepared recovery cannot succeed
   by guessing around missing infrastructure.
5. **Capacity is not pre-reserved.** Staging can retain encrypted and decrypted
   inputs, extracted files, a restored database, the original database, and
   previous uploaded files simultaneously. There is no explicit free-space
   preflight or expansion quota.
6. **Plaintext exists during operations.** Backup intermediates and
   restore-stage volumes can contain unencrypted clinical data. Failure
   deliberately retains some recovery resources. Removal is not secure erasure.
7. **Restore is not zero-downtime.** Writers are stopped for cutover and remain
   stopped on some failures until the normal recovery/start sequence succeeds.
8. **Commitment changes the recovery choice.** Failure after commitment does
   not trigger automatic rollback, because the replacement may already contain
   new writes.
9. **The journal is not the data.** Deleting Docker recovery volumes/databases
   or losing the installation directory can remove what automated recovery
   needs. Keep both metadata and recovery resources while investigating.
10. **Compatibility has limits.** A dump plus uploaded files does not include
    all installation configuration, TLS trust state, image pins, plugins, or
    the OS keyring. This code does not prove arbitrary PostgreSQL-version,
    upstream-schema, plugin, or storage-format transitions safe.
11. **Native access remains powerful.** Process environments, Docker access,
    and filesystem access can expose recovery material. The UI gate and
    resource labels are coordination/ownership mechanisms, not protection
    against a hostile machine administrator.

For an operator, the practical recovery checklist is: identify the intended
database-only or DB + files scope; preserve the source files and separately stored
recovery file; use the application-mediated restore/start path; read the actual
error and pending-state behavior; and do not delete originals or metadata to
make a warning disappear. A separate, controlled restore exercise is still
needed to establish a site's recovery procedure.

## 11. What the existing tests demonstrate

No clinic commands, live data access, or tests were run to write this guide.
The following describes the checked-in test source, not observed results on
this machine or proof of real-world disaster recovery.

| Test file | What it exercises | Boundaries and skips |
| --- | --- | --- |
| [`crypto_test.go`](../app/internal/backup/crypto_test.go) | Recovery-file matching; public-only installation; non-overwrite; symlink/location rejection; certificate ownership and cleanup; real OpenSSL CMS round trip. | Uses synthetic data and production-sized keys. OpenSSL absence skips the CLI round trip; symlink cases skip on Windows. |
| [`restore_test.go`](../app/internal/backup/restore_test.go) | Unsafe input rejection before Docker; preparation failures preserving live data; rollback before commitment; committed recovery preserving new writes; missing/foreign-resource refusal; journal validation; environment-only secret values; manual scope; pairing rules; resumable cleanup. | Docker is a test double implemented by rerunning the Go test binary. Database IDs, file contents, and container states are synthetic. It does not exercise a real Docker activation/health sequence. POSIX fixture cases skip on Windows. |
| [`restore_archive_test.go`](../app/internal/backup/restore_archive_test.go) | Real Python validation of synthetic regular/corrupt/truncated/path-escaping/link archives. An isolated PostgreSQL fixture tests full-payload validation, transactional load, quoted database names, tagged-session termination, transactional rename failure, swap, and rollback. | Python case skips without `python3`. PostgreSQL case skips on Windows, as root, or without its local PostgreSQL tools. It starts its own local fixture with TCP listening disabled, not a live clinic. No Docker-volume or power-loss disaster is reproduced. |
| [`backup_script_test.go`](../app/internal/clinic/backup_script_test.go) | Scheduled/manual failure reporting; plaintext and publication cleanup; partial prune failures; zero/positive retention; top-level-only deletion; collision refusal; writer serialization and lock release before sleep. | Uses isolated folders and command wrappers. Dump, encryption, and archive commands are simulated, so it does not prove real encryption/compression or capture consistency. POSIX shell required; serialization cases skip without native `flock`. |

Two particularly useful regression distinctions are:

- The PostgreSQL fixture demonstrates a corrupt dump whose table of contents
  can still be listed, while full payload validation fails. This explains why
  restore must do more than reuse capture-time `--list`.
- The interruption fixtures distinguish `prepared` from `committed` and simulate
  writes after activation/rollback. They test that repeated recovery does not
  overwrite those newer writes.

`CARE_RESTORE_DOCKER_STATE` in `restore_test.go` is internal test-double
plumbing, not a switch granting permission to exercise real Docker.
The tests owned by this guide mainly use fixtures and prerequisite-based skips;
they do not expose a single opt-in "disaster test" mode.

Native and integration tests also have platform, tool, and fixture
prerequisites. Consult their source and
[development and release](development-and-release.md) before running them.
A skipped case is not a pass for that behavior, and exercising a fixture is
not evidence of a complete clinic backup/restore rehearsal. Documentation-only
changes do not require a live restore or a broad test run.

## 12. Current source file map

This is the complete Go-file inventory owned by this guide.

| Source file | Current role |
| --- | --- |
| [`app/internal/backup/store.go`](../app/internal/backup/store.go) | `Store` dependency fields, runner attachment through `New`, optional logging, and the Compose command helper. |
| [`app/internal/backup/crypto.go`](../app/internal/backup/crypto.go) | Recovery-file generation/validation, public certificate installation, location checks, certificate ownership, and owned-entry backup deletion. |
| [`app/app_recovery.go`](../app/app_recovery.go) | Native recovery exports, setup verification, single-use Desktop recovery codes, password changes, and offline reset throttling. |
| [`app/internal/backup/restore.go`](../app/internal/backup/restore.go) | `Backup` list shape, filename recognition/pairing, configured/external source validation, key selection, staging orchestration, cutover orchestration, activation, and finalization call. |
| [`app/internal/backup/restore_data.go`](../app/internal/backup/restore_data.go) | Database readiness/settings/identity helpers, SQL load/swap/rollback/cleanup scripts, file snapshot/replacement helpers, and Python archive validation. |
| [`app/internal/backup/restore_journal.go`](../app/internal/backup/restore_journal.go) | Restore IDs/tags, journal schema/validation/durable writes, helper/writer shutdown, Docker-volume ownership, pending detection, recovery, and final cleanup. |
| [`app/internal/backup/crypto_test.go`](../app/internal/backup/crypto_test.go) | Key preservation, non-overwrite, foreign data, safe location/deletion, and certificate recovery regressions. |
| [`app/internal/backup/restore_test.go`](../app/internal/backup/restore_test.go) | Synthetic Docker/state fixture and restore input, interruption, commitment, cleanup, ownership, secret-transport, and listing regressions. |
| [`app/internal/backup/restore_archive_test.go`](../app/internal/backup/restore_archive_test.go) | Synthetic archive tests through Python plus tool-gated, isolated PostgreSQL payload/transaction/session/swap regressions. |
| [`app/internal/clinic/backup.go`](../app/internal/clinic/backup.go) | `Clinic.BackupNow`: encryption-presence guard, timestamp generation, existing-sidecar one-shot execution, and database-only completion logging. |
| [`app/internal/clinic/backupstore.go`](../app/internal/clinic/backupstore.go) | Compose project constant, clinic-to-store dependency/callback wiring, effective backup path, and recovery-before-sidecar-restart. |
| [`app/internal/clinic/backup_script_test.go`](../app/internal/clinic/backup_script_test.go) | Isolated shell-script fixtures for success/failure reporting, retention, publication, collision checks, manual scope, and locking. |

The two runtime deployment sources in scope are:

| Source file | Current role |
| --- | --- |
| [`deployments/scripts/backup.sh`](../deployments/scripts/backup.sh) | Unattended loop and manual one-shot mode; free-space check, encrypted database/files publication, advisory locking, temporary cleanup, retention, and the `backup-status` file. |
| [`storage/backup.go`](../app/internal/storage/backup.go) | Newest-set sizing, `BackupNeed`, days-left estimate, and `backup-status` parsing. |
| [`deployments/backup.Dockerfile`](../deployments/backup.Dockerfile) | PostgreSQL-image-derived tool image with OpenSSL added. |

### Current restore architecture

The current implementation has `restore_data.go` and `restore_journal.go`,
`BackendImage` and `EnsureRestoreImages` dependencies, and a
`Migrate(database, restoreID)` callback. It stages and migrates a separate
database, retains originals, records the four phases, and finalizes after
successful startup. There is no current `restoreDB`/`restoreFiles` live
drop-and-load path or old `waitForDB` helper in this package.

Follow the current sources and this guide's staged protocol rather than
historical direct-replacement diagrams. Silo continues to use the existing
`minio` service and volume names; no migration or renaming is implied.
