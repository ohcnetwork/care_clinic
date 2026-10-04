# `app/` — the CARE Clinic control app (Go / Wails)

The Go/Wails application operates the clinic through Docker Compose and native
system tools. The engine packages under `internal/` never import Wails.

On a new computer, CARE Clinic first asks whether to set up a clinic server or
connect as a client. A **Back** button on the next screen undoes a misclick as
long as nothing has been installed or connected; after that the selected role
stays locked until successful uninstall returns to the role choice.
Failed-install cleanup retains it. Existing and partial server installations retain
their server role. Clients enter a clinic address, approve the operating system's
certificate trust prompt, and open CARE in their browser, without installing or
starting Docker. They can retry the saved address or remove clinic access before
connecting to a different address. No fingerprint confirmation is required.

```bash
wails dev
```

For a production build, synchronize installer metadata before Wails starts:

```bash
node frontend/scripts/stage-install.mjs
wails build
```

See [Development and release](../docs/development-and-release.md) for preparing
a fresh checkout, embedded assets, build prerequisites, and platform packaging.
Running a development app can operate an existing clinic on this computer.
For publishing installers, follow [Releasing CARE Clinic](../docs/releases.md).

## Backend documentation

Start at the [documentation index](../docs/README.md), then read the
[architecture](../docs/architecture.md), [file map](../docs/repository-map.md),
[Wails API](../docs/wails-application.md), and
[configuration guide](../docs/configuration-and-settings.md).
