# CARE Desktop

Self-contained, offline [CARE](https://github.com/ohcnetwork/care) for a small clinic.
One installer on one computer runs the whole EMR — backend, web app, database, file
storage and automatic daily encrypted backups — and staff connect from other computers
using CARE Desktop in client mode on the clinic Wi-Fi. The core clinic can run
without internet after installation. Updates, externally hosted plugins such
as CARE Onboarding, and configured online services still need connectivity.

**Download:** [releases](https://github.com/ohcnetwork/care_desktop/releases)
— `.dmg` for macOS (Apple Silicon and Intel), `-setup.exe` for Windows 64-bit.

**Previously hosted CARE on this computer, but now cannot open another clinic?**
See [client recovery and earlier-install cleanup](docs/client-recovery.md).

## What it does

- **First-run choice** — **Start setup** to host a clinic, or **Connect to an
  existing server on the local network**. The start screen and its buttons save
  nothing: the server role is recorded when the setup screen begins, before any
  setup settings are saved. The client role is recorded only when a connection
  is attempted. Once written, ordinary use does not switch roles.
- **Server setup wizard** — platform-aware, sequential computer checks, clinic
  address, backup location and verified recovery file, admin password and six
  recovery codes, then Review. Review rechecks all requirements before accepting
  Install. Installation shows real activity rather than an invented percentage
  and opens the control panel directly when it succeeds.
- **Interrupted installation** — reconnect and retry without clearing setup
  while the original desktop process remains open and its saved recovery files
  are available. Completed downloads and images can be reused; this is not
  byte-level download resumption. If that attempt is no longer available, CARE
  explains the cleanup required before starting again.
- **Windows save locations** — the default backup folder and native save dialogs
  use the Desktop Windows actually displays, including OneDrive redirection.
  Saved recovery files have an **Open folder** action. Explicitly configured
  backup locations are kept; other operating systems retain their folder behavior.
- **Server control panel** — start/stop/restart, start at login, clinic address and
  phone/tablet setup QR, with dedicated Backups, Storage, Updates and Advanced
  pages. Plugins retains its reviewed catalog/table layout.
- **Native client setup** — enter the clinic's `.local` address shown on its
  server. **Find clinic** checks it and changes nothing on the computer;
  **Connect** is the step that does. No Docker, Git, or mDNS advertising runs on
  the client. CARE downloads and validates the public certificate, verifies the
  server's TLS hostname before installation, requests OS administrator approval
  when needed, and automatically checks HTTPS before opening CARE. Later
  connections use the saved certificate pin rather than silently downloading a
  replacement.
- **Connecting cleans up after the last time** — it removes CARE's leftover
  address overrides and every other `CARE Desktop Local CA` certificate this
  computer trusts, then installs the current clinic's. No extra confirmation:
  your computer asks for permission once (on a Mac, sometimes twice — once for
  the address, once for the certificate). A computer that used to host a clinic
  can therefore become an ordinary client without a separate cleanup, and the
  certificate being installed is never removed by its own cleanup, so
  reconnecting to the same clinic changes nothing.
- **Connected screen** — once connected, CARE Desktop keeps a light check on
  whether the clinic is answering, so staff can see at a glance that it is up
  before they click through. It only looks; it changes nothing.
- **Back** — walks through earlier setup steps; from the first step it can undo
  an unused setup. Client Back remains available until connected. Running work
  blocks navigation; the backend rejects clearing a setup or connection already
  in use. Review's Edit/Fix actions return directly to Review.
- **Disconnect** — disconnect a client and remove only the certificate
  it installed, without touching server data. Successful uninstall clears its role
  and returns to the Server/Client choice, as does successful server uninstall.
  Remove access before connecting to another clinic or uninstalling the desktop
  app through the operating system. Trusted certificates that did not come from
  CARE remain and may still permit browser access. Uninstall the setup in CARE
  Desktop before removing the executable through the operating system.
- **Backups** — encrypted backups every 24 hours, with actual configured retention,
  backup-now and one explicit **Restore from a backup file** path for local or
  imported files. Restoring requires the Desktop admin password, a recovery file
  for encrypted backups, and acknowledgement that clinic records will be replaced.
- **Advanced** — ten everyday choices grouped into Clinic details, Patients and
  visits, Billing, Backups and Staff access, plus Desktop password recovery,
  log, rebuild and uninstall. Less-used local options keep their defaults or saved
  values and can be overridden in **Extra settings (for support)**.
  Advanced locks 15 minutes after unlocking, or sooner
  when you leave the tab; protected native actions still check the password.
  Email, SMS/patient sign-in and MFA configuration are not exposed in this editor.
- **Short permission prompts** — CARE's in-window confirmations explain the
  next action without a technical checklist. System password and certificate
  approvals still appear in the operating system's own dialogs.

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
| `.github/workflows/` | CI, and releases started when `CARE_DESKTOP_VERSION` in `deployments/.env` changes on `main` |

**Using the app:** [Desktop workflows](docs/desktop-workflows.md) covers setup,
connection, installation, each control-panel tab and update handoffs.

**Technical documentation:** [Start with `docs/README.md`](docs/README.md) for
the architecture, file map, Wails API, configuration, lifecycle, backups, native
integrations, safe UI tests and release workflow.

**Releases:** [Preparing and publishing a version](docs/releases.md), including
releases started on merge, automatic tags, and macOS and Windows signing configuration.

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

CARE Desktop has no telemetry, analytics or crash reporting. Network requests
are made for setup, configured features and updates, including automatic update
checks:

- **Server setup** installs Docker (Rancher Desktop on macOS/Windows) and Git if they are missing (pinned,
  checksum-verified installers from github.com, or the operating system's own
  package tools), clones the CARE backend
  and frontend from github.com, pulls the PostgreSQL, Redis, Silo and Caddy images
  from Docker Hub, and builds the CARE images, fetching their package dependencies.
- **The Windows installer** contains Microsoft's WebView2 bootstrapper, which
  downloads the WebView2 runtime from Microsoft if the computer lacks it.
- **Clinic features** the operator turns on, such as SMS sign-in codes and email,
  send data to the provider the operator configured.
- **Updates** check GitHub release metadata for CARE Desktop. While the clinic
  is serving, CARE's background update checker can resolve its configured source
  branches and build newer images; those builds fetch dependencies.
- **Hosted plugins**, including the default CARE Onboarding plugin for new
  clinics, are downloaded by staff browsers from their configured hosts.

On the clinic network the server announces `https://<clinic>.local` with mDNS so
staff computers can find it; clients do not advertise a clinic. Clients retrieve
the public certificate over local HTTP and check the clinic over HTTPS. That
traffic stays on the local network. Rancher
Desktop, the container images and WebView2 are covered by their own privacy
policies.

Build with `cd app && node frontend/scripts/stage-install.mjs && wails build`
(needs Go, Node 22 and the Wails CLI).

Run `cd app && wails dev` to review the real desktop application. This uses the
native backend and can operate an existing clinic on this computer; use an
isolated environment for destructive installation, restore and removal tests.

Run `cd app/frontend && npm run test:ui` for the Playwright scenarios
(requires Playwright's Chromium browser; install it with `npx playwright install chromium`
if it is missing). They cover navigation, native-contract gates, recovery,
installation and update events, panel actions, settings/plugins, errors,
keyboard access, and layout at 1100×700 and 720×560 using a simulated host under
`app/frontend/tests/fixtures/`. The runner starts its own loopback server on port
41783. These fixtures require Vite test mode, perform no native operations, and
are excluded from the production entry point.

MIT licensed — see [LICENSE](LICENSE). Part of the [Open Healthcare Network](https://ohc.network).
