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
  and exposes it to the computer. It keeps running when the window closes, so
  the clinic stays up; quitting CARE Clinic from the menu stops it.
- **Run.** The app points every `docker` / `docker compose` call at the
  engine with `DOCKER_HOST`, `DOCKER_CONFIG` and a `PATH` that puts the
  engine's own CLI first (`vm.Env`). Nothing reads the user's Docker contexts.
- **Ports.** The helper listens on ports 80 and 443 and forwards them into the
  VM, where Caddy publishes the clinic. Only those two ports are forwarded.
- **Update.** When `CARE_VM_VERSION` changes in a new app release, the setup
  check shows "Update CARE engine". App self-updates leave a running engine in
  place, so on its first launch the new app compares the installed and running
  engine with the pin and updates it automatically before reporting clinic
  status (the panel shows "Updating" rather than "Running"). It downloads and
  verifies the new engine first, then removes the clinic containers (volumes
  kept), stops the old helper, installs the pinned engine, starts it and starts
  the clinic. If the update fails, the clinic is started on the previous engine
  and Overview and Logs show "CARE engine update required" with a retry button. The data disk (images,
  volumes, clinic data) is kept; only the engine files are replaced.

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
