# CARE Desktop

Self-contained, offline [CARE](https://github.com/ohcnetwork/care) for a small clinic.
One installer on one computer runs the whole EMR — backend, web app, database, file
storage and nightly encrypted backups — and staff connect from other computers
using CARE Desktop in client mode on the clinic Wi-Fi. No cloud account, and no internet
needed after the first install.

**Download:** [releases](https://github.com/ohcnetwork/care_desktop/releases)
— `.dmg` for macOS (Apple Silicon and Intel), `-setup.exe` for Windows 64-bit.

**Previously hosted CARE on this computer, but now cannot open another clinic?**
See [client recovery and earlier-install cleanup](docs/client-recovery.md).

## What it does

- **First-run choice** — choose **Server** to host a clinic or **Client** to
  connect to one. The role is saved; ordinary use does not switch roles.
- **Server setup wizard** — checks the computer (Docker, Git, network), takes a backup
  folder and password and an admin login, then builds and starts CARE.
- **Server control panel** — start/stop/restart, start at login, and the clinic address.
- **Native client setup** — enter the clinic's `.local` address shown on its
  server. No Docker, Git, or mDNS advertising runs on the client. CARE downloads
  and validates the public certificate, verifies the server's TLS hostname
  before installation, requests OS administrator approval when needed, and
  automatically checks HTTPS before opening CARE. Later connections use the
  saved certificate pin rather than silently downloading a replacement.
- **Back** — on the server setup and client screens, returns to the Server/Client
  choice after a misclick. It is only offered while nothing has been installed
  or connected; after that, uninstall is the way back.
- **Disconnect** — disconnect a client and remove only the certificate
  it installed, without touching server data. Successful uninstall clears its role
  and returns to the Server/Client choice, as does successful server uninstall.
  Remove access before connecting to another clinic or uninstalling the desktop
  app through the operating system. Pre-existing trusted certificates remain
  and may still permit browser access. Uninstall the setup in CARE Desktop before
  removing the executable through the operating system.
- **Backups** — nightly encrypted backups of the database and uploaded files,
  back up now, restore from the backup folder or an imported file.
- **Advanced** — plain-language clinic settings (backups, sign-in, patient SMS
  codes, email, branding, languages, visits, billing), backend plugins, log, uninstall.

## How it works

Client setup uses trust on first use over the local network: the initial
certificate download is HTTP, not authenticated HTTPS. Use a trusted clinic
network and the address supplied by its administrator. Automatic certificate and
TLS checks do not prove that an attacker did not impersonate the server during
that first download. There is no manual fingerprint-comparison step.
Phones and tablets use `http://<clinic>.local/setup` for iOS/Android certificate
steps, opened from **Connect phone or tablet** in the server panel. Windows/Mac
clients keep using the desktop app; downloadable scripts remain retired. Separate clinic
servers with unique names remain valid; CARE does not enforce a signed,
network-wide single-clinic rule.

A Go / [Wails](https://wails.io) desktop app with a React UI drives a Docker Compose
stack: the CARE backend and workers, the CARE frontend, PostgreSQL, Redis, Silo for
files, and Caddy with the Coraza WAF as the HTTPS front door. Both CARE images are
built on the clinic's machine from the upstream commits pinned in `deployments/.env`.

| Path | What lives there |
|---|---|
| `app/` | The desktop app: Go engine in `internal/`, Wails bindings in `*.go`, React UI in `frontend/` |
| `deployments/` | The server kit: compose file, Caddyfile, env files, backup script, and public certificate bootstrap route |
| `.github/workflows/` | CI and manually triggered releases using the version and pins in `deployments/.env` |

**Backend documentation:** [Start with `docs/README.md`](docs/README.md) for the
architecture, file map, Wails API, configuration, lifecycle, backups, native
integrations and release workflow.

**Releases:** [Preparing and publishing a version](docs/releases.md), including
manual Actions runs, automatic tags, and macOS and Windows signing configuration.

## Code signing policy

Free code signing provided by [SignPath.io](https://signpath.io), certificate by
[SignPath Foundation](https://signpath.org).

Windows releases are built by [GitHub Actions](.github/workflows/release.yml)
from a commit of this repository and signed by SignPath only after it has
verified that the file came from that build.

| Role | Members |
|---|---|
| Authors — commit to this repository | [@praffq](https://github.com/praffq) |
| Reviewers — review pull requests | [@praffq](https://github.com/praffq) |
| Approvers — approve signing of a release | [@praffq](https://github.com/praffq) |

### Privacy

This program will not transfer any information to other networked systems unless
specifically requested by the user or the person installing or operating it.

CARE Desktop has no telemetry, analytics or crash reporting. It uses the internet
only when the person setting up or operating the clinic asks for something that
needs it:

- **Server setup** installs Docker (Rancher Desktop on macOS/Windows) and Git if they are missing (pinned,
  checksum-verified installers from github.com, or the operating system's own
  package tools), clones the CARE backend
  and frontend from github.com, pulls the PostgreSQL, Redis, Silo and Caddy images
  from Docker Hub, and builds the CARE images, fetching their package dependencies.
- **The Windows installer** contains Microsoft's WebView2 bootstrapper, which
  downloads the WebView2 runtime from Microsoft if the computer lacks it.
- **Clinic features** the operator turns on, such as SMS sign-in codes and email,
  send data to the provider the operator configured.

On the clinic network the server announces `https://<clinic>.local` with mDNS so
staff computers can find it; clients do not advertise a clinic. Clients retrieve
the public certificate over local HTTP and check the clinic over HTTPS. That
traffic stays on the local network. Rancher
Desktop, the container images and WebView2 are covered by their own privacy
policies.

Build with `cd app && node frontend/scripts/stage-install.mjs && wails build`
(needs Go, Node 22 and the Wails CLI). MIT
licensed — see [LICENSE](LICENSE). Part of the [Open Healthcare Network](https://ohc.network).
