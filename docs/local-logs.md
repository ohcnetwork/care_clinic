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

Logs rotate at 10 MiB with four retained stream files. A minute-by-minute sweep
expires logs after seven days and enforces a shared 500 MiB budget. The limit can
be briefly exceeded between sweeps. The guest journal also has a 200 MiB persistent
budget and seven-day retention. Capture is best-effort: a hard crash, disk failure,
journal expiry or a prolonged disconnection can lose messages; gaps are reported
where detectable. Container names, rather than fixed service names, distinguish
replicas and recreated services.
Serial capture uses a bounded queue and Docker logging uses non-blocking buffers:
logging must not stall the VM or containers if the host disk falls behind.

**Open logs folder** opens the shared folder. **Export diagnostics** saves a ZIP
of retained logs and app/engine version information, after a privacy warning.
No automatic upload occurs. Exports exclude environment files, database contents,
credentials and full journal metadata, but logged messages can still contain
patient information, URLs or secrets printed by services. Review before sharing.
Export snapshots may omit messages written during export or rotation.

GitHub Actions build logs remain on GitHub and are not included in local exports.
