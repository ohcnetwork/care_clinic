# Local diagnostics

CARE Clinic keeps local logs in one private folder:

- macOS: `~/Library/Logs/care-clinic/`
- Windows: `%LOCALAPPDATA%\care-clinic\logs\`
- Linux: `$XDG_STATE_HOME/care-clinic/` (default `~/.local/state/care-clinic/`)

`care-log.log` records app operations, installation, updates and backups.
`app-update-helper.log` preserves installer/relaunch diagnostics after the update
staging directory is removed.
`engine-host.log` records the VM lifecycle and host connections.
`engine-console.log` captures serial boot/kernel output.
`guest-system.log` captures guest system services, including Docker.
`containers/<container-name>.log` captures container stdout/stderr.
`collector.log` records collection interruptions, reconnects and retention failures.

The engine helper copies the guest journal to the host continuously, independently
of the app window. It reconnects after interruptions and replays from its last
journal cursor where available. Guest logging requires the rebuilt engine with
the `/v1/logs` agent endpoint and Docker's journald logging driver. Existing
containers retain their original logging driver until recreated.

The Logs page shows the live collection state reported by the helper
(collecting, connecting, reconnecting, engine stopped). If an older engine is
still running after an app update, the app updates it automatically on launch
and recreates the containers so they use journald. If that fails, the page shows
"CARE engine update required" with an **Update CARE engine** retry.

Linux runs Docker directly: container logs stay in the Docker daemon and can be
read with `docker compose logs` from the installation directory. Guest/VM
collection applies only to the app-managed engine on macOS and Windows.

Logs rotate at 10 MiB with four retained stream files. A minute-by-minute sweep
expires files inactive for seven days (by modification time) and enforces a
shared 500 MiB budget, reclaiming oldest files first. Active logs are truncated
rather than unlinked. This does not remove individual old records inside a
recently modified file. The limit can
be briefly exceeded between sweeps. The guest journal also has a 200 MiB persistent
budget and seven-day retention. Capture is best-effort: a hard crash, disk failure,
journal expiry or a prolonged disconnection can lose messages; gaps are reported
where detectable. Container names, rather than fixed service names, distinguish
replicas and recreated services.
Serial capture uses a bounded queue and Docker logging uses non-blocking buffers:
logging must not stall the VM or containers if the host disk falls behind.

The **Logs** sidebar page shows the folder path, log categories, retention limits
and sharing guidance. **Open logs folder** opens the shared folder. **Export diagnostics** saves a ZIP
of retained logs and app/engine version information, after a privacy warning.
No automatic upload occurs. Exports exclude environment files, database contents,
credentials and full journal metadata, but logged messages can still contain
patient information, URLs or secrets printed by services. Review before sharing.
Export snapshots may omit messages written during export or rotation. They include
only the last 10 MiB per file, with a 500 MiB combined log limit, and reject symlinks
and destinations inside the log directory. `versions.json` records the app's
engine pin, not a measurement of the running engine version; check the live
engine status when diagnosing a version mismatch.

GitHub Actions build logs remain on GitHub and are not included in local exports.
