# The CARE engine (Docker on macOS and Windows)

[Documentation index](README.md)

CARE runs as Docker containers. Linux servers run Docker natively. macOS and
Windows can't run Linux containers directly, so CARE Clinic ships its own small
Linux virtual machine, the **CARE engine**. It runs only Docker and a tiny agent.
Earlier versions used Rancher Desktop (and WSL 2 on Windows). The engine
replaces both: nothing else to install, no separate app in the menu bar, and
one version that CARE Clinic controls.

```mermaid
flowchart LR
  subgraph Host["Your computer (macOS / Windows)"]
    UI["CARE Clinic app"] -->|docker / docker compose CLI| Sock["Docker endpoint"]
    UI -->|start / stop / status| Helper["care-clinic --vm-host<br/>(engine helper)"]
    Helper --> Sock
    Browser["Browser"] -->|:80 / :443| Helper
  end
  subgraph VM["CARE engine (Linux VM)"]
    Agent["care-agent"]
    Dockerd["dockerd"] --> Stack["CARE containers<br/>(caddy, backend, frontend, db, ...)"]
  end
  Helper -->|vsock (macOS) / forwarded TCP + mTLS (Windows)| Dockerd
  Helper --> Agent
```

## What the app does

- **Install.** The setup screen's Docker step installs the engine: a
  `care-engine-<version>-linux-<arch>.tar.gz` bundle (kernel, initrd, a
  read-only system image and an empty data-disk seed), the `docker` CLI and
  the `docker compose` plugin, plus QEMU on Windows. Every file comes from a
  version and SHA-256 pinned in `deployments/.env`.
- **Start.** `vm.Ensure` launches the engine helper (`care-clinic --vm-host`,
  the same executable). The helper boots the VM, waits for Docker inside it,
  and exposes it to the computer. Quitting a running clinic offers **Leave
  running** or **Stop**. Stop stops containers and the VM; leaving it running
  preserves them, but the app's mDNS advertisement stops, so other devices'
  `.local` access may fail until the app reopens.
- **Run.** The app points every `docker` / `docker compose` call at the
  engine with `DOCKER_HOST`, `DOCKER_CONFIG` and a `PATH` that puts the
  engine's own CLI first (`vm.Env`). Nothing reads the user's Docker contexts.
- **Ports.** The helper listens on ports 80 and 443 and forwards them into the
  VM, where Caddy publishes the clinic. Only those two ports are forwarded.
- **Update.** When `CARE_VM_VERSION` changes in a new app release, the setup
  check shows "Update CARE engine". App self-updates leave a running engine in
  place, so on panel launch the app compares the installed and running
  engine with the pin and updates it automatically before reporting clinic
  status (the panel shows "Updating" rather than "Running"). An unfinished
  restore or plugin rollback takes priority and skips this automatic upgrade
  until recovery completes. The data disk (images, volumes, clinic data) is
  kept. The update is one transaction; see [Engine update rollback](#engine-update-rollback).

### Engine update rollback

`prereq.installEngineThen` and [`vm/update.go`](../app/internal/vm/update.go)
replace the engine bundle and its host tools (docker CLI, compose plugin and,
on Windows, QEMU) together:

1. **Stage.** Every download is checksum-verified, then unpacked and the
   bundle's per-file hashes checked into `engine-update/new/`. The running
   clinic is untouched; a failure here only reports the error.
2. **Activate.** The clinic containers are removed (volumes kept) and the VM
   stopped. `engine-update/record.json` is written before anything is moved.
   The current tools move to `engine-update/old/`, the new ones into place,
   the new bundle into `engine/<version>/`, and `engine/current` is replaced
   atomically. The previous bundle stays on disk.
3. **Verify.** The new engine is started; Docker must answer (including
   `docker compose`) and the helper must report the new version.
4. **Commit or roll back.** On success the record is removed and the previous
   bundle and tools are deleted; CARE is then started. If activation or
   verification fails, the partly started VM is stopped, the previous tools
   and `current` pointer are put back, the new bundle is deleted, the record is
   marked `failed` with the cause, and the clinic is started on the previous
   engine. Errors from the update, the restore and the restart are all reported.

If CARE Clinic quits or the computer loses power between activation and
commit, the next launch stops the engine (its helper outlives the app) and
rolls back before anything else uses it. The same recovery runs before every
engine start and Docker-readiness check, so a recovery that failed once is
tried again before CARE starts.

A failed update is not retried automatically: the panel shows **CARE engine
update required** with the cause and a **Try the update again** button. The
retry clears the failed record and runs the whole transaction again. A newer
CARE Clinic that pins a different engine updates automatically as usual.
Download failures change nothing and leave no failed record.

Limits:

- The data disk is shared by both engines and is never copied or reverted.
  Anything the new engine wrote to it (for example Docker metadata) stays.
- Once the new engine and Docker answer, the update is committed. A later CARE
  start or migration failure is handled by normal CARE recovery and restore,
  not by going back to the previous engine. Database migrations are never
  rolled back by an engine rollback.
- A first install has no previous engine; a failed first install removes the
  partial install instead.

## Platforms

| | macOS (11+, Apple silicon and Intel) | Windows 10/11 (x64) |
| --- | --- | --- |
| Hypervisor | Apple Virtualization framework (built in) | QEMU with Windows Hypervisor Platform (WHPX) |
| One-time setup | None | Turn on *Windows Hypervisor Platform* (admin prompt, then restart). Virtualization must be enabled in the BIOS/UEFI. |
| Docker endpoint | Unix socket `~/.care-clinic/run/docker.sock`, carried over vsock | `tcp://127.0.0.1:<port>` with mutual TLS (keys created per install) |
| Host folders in containers | Shared live with virtiofs at the **same path** (your home folder and `/Volumes`) | Mirrored: the install and backup folders are copied into the VM and synced both ways |
| App signing | Needs the `com.apple.security.virtualization` entitlement (`app/build/darwin/entitlements.plist`) | None |

### Windows folder sync

The VM on Windows can't see the computer's folders, so the helper keeps a copy
of the install folder and the backup folder inside the VM. It syncs before and
after every Docker command that uses those files (compose up, backups,
restores) and every 15 seconds in between. When the same file changed on both
sides since the last sync, the computer's copy wins.

## Where things live

| | macOS | Windows |
| --- | --- | --- |
| Engine state (engine files, data disk, logs, config) | `~/Library/Application Support/care-clinic/vm` | `%LOCALAPPDATA%\care-clinic\vm` |
| Sockets | `~/.care-clinic/run` | `%LOCALAPPDATA%\care-clinic\vm\run` |

Inside that folder: `engine/` (the installed bundle and `manifest.json`),
`engine-update/` (only while an update is staged, being verified, or after it
failed),
`data.img` (the sparse data disk that holds Docker's images, containers and
volumes; it grows as needed, up to 64 GB by default), `bin/` (docker, compose,
QEMU) and `config.json` (CPU/memory/disk sizing). Logs live in the shared
[local diagnostics folder](local-logs.md), outside the engine state folder.

The storage tab reports this folder as the **CARE engine disk**. Uninstalling
CARE with "remove images" deletes the folder.

## Troubleshooting

- **Logs.** `engine-host.log` (the helper) and `engine-console.log` (boot console)
  are in the shared logs folder alongside continuously captured guest and
  container logs. If the engine fails to start, the app also
  writes `care-engine-diagnostics.txt` (helper log, console and guest journal)
  next to its own `care-log.log`. See [local diagnostics](local-logs.md).
- **"Windows Hypervisor Platform is off".** Use the *Turn on* button on the
  setup screen, then restart. If it says virtualization is disabled, turn on
  Intel VT-x / AMD-V (sometimes called "SVM") in the computer's BIOS/UEFI.
- **The clinic page doesn't open.** Another program may be using port 80 or
  443 (`engine-host.log` says `port 80: address already in use`). Stop it (common
  culprits: IIS, Skype, another web server, an old Rancher Desktop) and restart
  the engine from the app.
- **Windows firewall prompt.** The first start may ask to allow QEMU. The
  clinic needs it allowed on private networks so other computers can reach it.
- **Start over.** Quit CARE Clinic, delete the engine state folder and open
  the app again. The setup screen reinstalls the engine. This deletes all
  Docker data, so take a backup first.

## Moving from Rancher Desktop

Earlier CARE Clinic versions kept clinic data in Rancher Desktop's Docker. The
engine starts with an empty disk, so:

1. On the old version, take a backup (Backups tab).
2. Install the new version and let it set up the CARE engine.
3. Restore the backup.
4. Optionally remove Rancher Desktop: Advanced → Uninstall offers "Also remove
   Rancher Desktop left by an older CARE Clinic", or uninstall it yourself if
   nothing else uses it.

## Building the engine

The engine is defined in `vm/`:

- `vm/flake.nix`, `vm/nix/guest.nix`: a minimal NixOS system (built with
  [microvm.nix](https://github.com/microvm-nix/microvm.nix)) running only
  dockerd and the agent, with a read-only erofs system image and an ext4 data
  disk that grows on boot.
- `vm/agent/`: `care-agent`, a small Go service in the guest that reports
  status, mounts shares, reads logs, sets the clock after sleep and serves the
  Windows sync.
- `vm/nix/bundle.nix`: packs the kernel, initrd, system image and data seed
  into the release tarball with a `manifest.json` of checksums.
- `vm/VERSION`: the engine version.
- `vm/qemu-windows.json`: the exact upstream Windows installer URL, QEMU
  version and SHA-512.
- `vm/package_qemu.py`, `vm/validate_qemu_windows.ps1`: reproducible Windows
  support-bundle packaging and native Windows validation.

The app pins a small patch of `Code-Hex/vz` in `app/go.mod`: its Objective-C
`AppDelegate` is renamed to `VZAppDelegate` to avoid a linker collision with
Wails. The replacement points at a specific commit in `praffq/vz`, not a moving
branch.

To release a new engine: bump `vm/VERSION`, run the **CARE engine** workflow
(`.github/workflows/care-vm.yml`), then paste the values it prints into
`deployments/.env` and ship an app release. Locally (needs Docker; takes
about 15 minutes the first time):

```sh
docker volume create care-nix-store
docker run --rm -v care-nix-store:/nix -v "$PWD/vm":/src:ro -v "$PWD/out":/out nixos/nix sh -c \
  'cp -r /src /build && cd /build && nix --extra-experimental-features "nix-command flakes" \
   --option filter-syscalls false build .#bundle -L && cp -L result/* /out/'
```

This builds for the architecture Docker runs on (arm64 on Apple silicon).

### Packaging QEMU for Windows

The Windows driver runs `qemu-system-x86_64.exe` from the ZIP root and passes
`-L <bundle>/share`. The ZIP must include its DLL dependencies, firmware and
support directories, not just the executable. The package script retains every
top-level DLL, `share/`, `lib/` when present, `COPYING`, `COPYING.LIB`, `README.rst`
and `VERSION`; it excludes other emulators and NSIS installer plugins.

The source is Stefan Weil's Windows distribution, linked from
[QEMU's download page](https://www.qemu.org/download/#windows), not an official
QEMU-project Windows binary build. The pinned archived installer is
[20250826 (10.1.0)](https://qemu.weilnetz.de/w64/2025/).
Its published SHA-512 is checked against the repository pin **before**
extraction. This establishes consistency with the distributor's HTTPS checksum,
not an independent publisher signature; the distributor warns that newer
installers use an expired signing certificate.

With Python 3 and 7-Zip available:

```sh
python -B -m unittest discover -s vm -p test_package_qemu.py
python -B vm/package_qemu.py --output out
```

The script can reuse a download with `--installer <path>`; the same checksum
check still applies. ZIP entries are stored without compression, in sorted
order with fixed metadata, so the same input produces the same bytes across
hosts. This trades size (about 463 MiB) for reproducibility. The ZIP includes
`care-qemu-provenance.json` with the source URL, source SHA-512 and each bundled
file's SHA-256.

Before upload, run on Windows with PowerShell 7:

```powershell
./vm/validate_qemu_windows.ps1 -Archive out/qemu-10.1.0-windows-x86_64.zip
```

This verifies the bundled files and version, requires compiled WHPX support,
and smoke-tests q35 firmware, virtio storage/network/RNG and user-mode
networking under TCG. It does **not** establish an actual WHPX guest boot;
that still requires a disposable Windows machine with WHPX enabled.

Upload only the Windows-validated ZIP to the release selected by
`CARE_VM_BASE_URL`, retain the existing engine tarballs unchanged, and regenerate
`SHA256SUMS` for the complete asset set. Never replace a different binary under
an existing version/checksum pin. The desktop release's all-platform download
verification must remain enabled. Retain license notices and arrange the
corresponding-source/license obligations for QEMU and its bundled dependencies
before public redistribution; checksum verification is not a licensing audit.

The prepared 10.1.0 bundle has SHA-256
`35e964db85f55a4a08786fb418c2572c4e22e5d370862987b0eecb82400d6364`,
now recorded in `deployments/.env`. It has **not** been Windows-validated or
uploaded yet. The published `care-vm.yml` currently makes Windows packaging
optional and defaults to a different, unpinned installer. A workflow patch is
provided separately for the GitHub configuration owner; the repository's
`.github/` files and desktop signing/release flow are unchanged.
