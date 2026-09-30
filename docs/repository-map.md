# Repository and file map

[Documentation index](README.md)

This is a navigation map of the Wails backend and its direct build/runtime contracts. Every Go source and test file is listed below. The desktop frontend is shown only where it connects to the backend; its visual component tree is outside this guide's scope.

## Repository-level structure

```text
care_desktop/
|-- README.md
|-- docs/
|   |-- README.md
|   |-- architecture.md
|   |-- repository-map.md
|   |-- wails-application.md
|   |-- configuration-and-settings.md
|   |-- plugins.md
|   |-- clinic-lifecycle.md
|   |-- backups-and-restore.md
|   |-- cleanup-and-uninstall.md
|   |-- native-integrations.md
|   |-- development-and-release.md
|   `-- onboarding.md
|-- app/
|   |-- main.go and app_*.go
|   |-- password.go
|   |-- go.mod
|   |-- go.sum
|   |-- wails.json
|   |-- .golangci.yml
|   |-- build/
|   |-- frontend/
|   |-- install/
|   `-- internal/
|-- deployments/
|   |-- .env
|   |-- backend.env
|   |-- frontend.env
|   |-- docker-compose.yml
|   |-- Caddyfile
|   |-- backup.Dockerfile
|   |-- caddy.Dockerfile
|   |-- scripts/backup.sh
|   |-- minio/entrypoint.sh
|   `-- setup/index.html
`-- .github/
    `-- workflows/
        |-- ci.yml
        `-- release.yml
```

`deployments/` is versioned kit source. `app/install/` is generated build staging. The clinic computer's installed kit is a third location selected by the application, not either of those checkout directories. [Facility setup](onboarding.md) and its curated datasets live in the separate `ohcnetwork/care_onboarding_fe` plugin repository.

## Application boundary: `app/*.go`

All of these files belong to Go `package main`, even though they are organized by feature.

| Source | Responsibility | Main guide |
| --- | --- | --- |
| [`main.go`](../app/main.go) | Embedded filesystems, log initialization, `wails.Run`, callbacks/binding, fatal startup handling. | [Wails application](wails-application.md). |
| [`app.go`](../app/app.go) | `App` state, construction, log/event delivery, mDNS advertiser ownership and watcher. | [Architecture](architecture.md), [Wails application](wails-application.md). |
| [`app_lifecycle.go`](../app/app_lifecycle.go) | Startup refresh, shutdown, second launch, close confirmation, bounded stop-for-quit. | [Wails application](wails-application.md). |
| [`app_config.go`](../app/app_config.go) | `Config`, OS configuration path, strict initial loading, cached/atomic saves and forgetting state. | [Configuration](configuration-and-settings.md). |
| [`app_client.go`](../app/app_client.go) | Native client connection, pinned trust/ownership journal, TLS verification, and exact-certificate uninstall with role reset after success. | [Native integrations](native-integrations.md), [Wails application](wails-application.md). |
| [`app_installdir.go`](../app/app_installdir.go) | Fixed runtime kit path, unpacking/preservation rules, creation of a configured `Clinic`. | [Configuration](configuration-and-settings.md). |
| [`app_actions.go`](../app/app_actions.go) | Read/write job gates, async runner, lifecycle/admin guards, setup, action dispatch, failed-install cleanup. | [Wails application](wails-application.md), [clinic lifecycle](clinic-lifecycle.md). |
| [`app_update.go`](../app/app_update.go) | CARE branch-update status/check/dismiss, the background update watcher, and the GitHub-release desktop updater. | [Clinic lifecycle](clinic-lifecycle.md), [releases](releases.md). |
| [`app_status.go`](../app/app_status.go) | State/health/tool/network queries, provisioning controls, name/password/folder validation, pre-setup naming. | [Wails application](wails-application.md), [native integrations](native-integrations.md). |
| [`app_ui.go`](../app/app_ui.go) | Native URL/folder/log actions and login-startup controls. | [Wails application](wails-application.md). |
| [`app_env.go`](../app/app_env.go) | Authorized reads and atomic writes of the two installed environment files. | [Configuration](configuration-and-settings.md). |
| [`app_plugins.go`](../app/app_plugins.go) | Authorized plugin configuration access through the domain manager. | [Configuration](configuration-and-settings.md). |
| [`app_backup.go`](../app/app_backup.go) | Listing/import inspection, restore dispatch, backup picker, and backup-directory changes. | [Backups](backups-and-restore.md), [Wails application](wails-application.md). |
| [`app_uninstall.go`](../app/app_uninstall.go) | Authorized normal uninstall, removal checkpoint, post-cleanup scan, local-state cleanup and event. | [Cleanup](cleanup-and-uninstall.md). |
| [`app_osremove.go`](../app/app_osremove.go) | `--uninstall`/`--uninstall-check` modes for the Windows uninstaller, the shared "still set up" check, and in-app removal of the desktop app. | [Cleanup](cleanup-and-uninstall.md#removing-the-desktop-app). |
| [`app_residue.go`](../app/app_residue.go) | Residue scanning, recovery of old install location, confirmed purge, preserving the selected first-run name. | [Cleanup](cleanup-and-uninstall.md). |
| [`password.go`](../app/password.go) | Shared setup password policy. | [Wails application](wails-application.md). |
| [`app_env_test.go`](../app/app_env_test.go) | Synthetic-app regressions for concurrent settings reads, guards, conflicting writes, and saved retention values. | [Configuration](configuration-and-settings.md). |

The complete method signatures and event/result shapes are in the [Wails API reference](wails-application.md#complete-bound-method-reference). Do not infer an exported API just from a filename: Wails binds exported `App` methods, not every function in this package.

## Internal package structure

```text
app/internal/
|-- backup/
|   |-- store.go
|   |-- crypto.go
|   |-- restore.go
|   |-- restore_data.go
|   |-- restore_journal.go
|   |-- crypto_test.go
|   |-- restore_test.go
|   `-- restore_archive_test.go
|-- clinic/
|   |-- clinic.go
|   |-- setup.go
|   |-- start.go
|   |-- stop.go
|   |-- status.go
|   |-- rebuild.go
|   |-- images.go
|   |-- freespace.go
|   |-- migrate.go
|   |-- domain.go
|   |-- secret.go
|   |-- backup.go
|   |-- backupstore.go
|   |-- caddyroot.go
|   |-- thiscomputer.go
|   |-- uninstall.go
|   |-- purge.go
|   |-- teardown.go
|   |-- leftovers.go
|   |-- backup_script_test.go
|   |-- domain_test.go
|   |-- freespace_test.go
|   |-- migrate_test.go
|   |-- teardown_test.go
|   `-- thiscomputer_test.go
|-- compose/
|   |-- build.go
|   |-- build_test.go
|   |-- deployment_test.go
|   |-- parallel.go
|   `-- parallel_test.go
|-- health/
|   `-- health.go
|-- plugins/
|   |-- catalog.yml
|   |-- plugins.go
|   `-- plugins_test.go
|-- prereq/
|   |-- check.go
|   |-- profile.go
|   |-- provision.go
|   `-- provision_test.go
|-- release/
|   |-- pins.go
|   |-- pins_test.go
|   `-- workflow_test.go
|-- residue/
|   |-- residue.go
|   `-- residue_test.go
`-- sys/
    |-- applog/
    |   |-- applog.go
    |   |-- dir.go
    |   `-- rotate.go
    |-- appremoval/
    |   |-- appremoval.go
    |   |-- trash_darwin.go
    |   |-- trash_other.go
    |   `-- appremoval_test.go
    |-- atomicfile/
    |   |-- atomicfile.go
    |   |-- replace_darwin.go
    |   |-- replace_linux.go
    |   |-- replace_windows.go
    |   `-- atomicfile_test.go
    |-- autostart/
    |   `-- autostart.go
    |-- elevate/
    |   |-- elevate.go
    |   `-- elevate_test.go
    |-- hosts/
    |   |-- hosts.go
    |   `-- hosts_test.go
    |-- mdns/
    |   |-- advertise.go
    |   |-- advertise_test.go
    |   |-- hostname_network_test.go
    |   |-- probe.go
    |   `-- responder.go
    |-- netfix/
    |   |-- netfix.go
    |   `-- netfix_test.go
    |-- proc/
    |   |-- proc.go
    |   |-- console_other.go
    |   |-- console_windows.go
    |   `-- proc_test.go
    |-- reboot/
    |   `-- reboot.go
    `-- trust/
        |-- trust.go
        |-- client.go
        `-- trust_test.go
```

### Where each package is explained

| Package | Responsibility | Detailed explanation and source-file roles |
| --- | --- | --- |
| [`internal/clinic`](../app/internal/clinic) | Orders operations across the domains; no Wails dependency. | [Clinic lifecycle](clinic-lifecycle.md), [cleanup](cleanup-and-uninstall.md), [backup callbacks](backups-and-restore.md), [local device access](native-integrations.md). |
| [`internal/backup`](../app/internal/backup) | Backup inventory, recovery files/public certificates, decryption, staged replacement, journal recovery. | [Backups and restore](backups-and-restore.md). |
| [`internal/compose`](../app/internal/compose) | Infrastructure and CARE image building, source checkout, freshness inputs. | [Clinic lifecycle](clinic-lifecycle.md). |
| [`internal/health`](../app/internal/health) | HTTP readiness and port availability. | [Native integrations](native-integrations.md). |
| [`internal/plugins`](../app/internal/plugins) | Desktop plugin list (`plugins.json`), bundled catalog, validation, derived `ADDITIONAL_PLUGS`, and frontend `PlugConfig` rows. | [Configuration](plugins.md). |
| [`internal/prereq`](../app/internal/prereq) | Docker/Git detection, action plans, Rancher Desktop provisioning and preconfiguration, and readiness waits. | [Native integrations](native-integrations.md). |
| [`internal/release`](../app/internal/release) | Validated release manifest and source/image identity. | [Development and release](development-and-release.md#release-identity-and-pins). |
| [`internal/residue`](../app/internal/residue) | Owned-resource inventory, unknown-state errors, old kit location. | [Cleanup](cleanup-and-uninstall.md). |
| [`sys/proc`](../app/internal/sys/proc) | Child-process creation, runner context/output, PATH repair. | [Native integrations](native-integrations.md). |
| [`sys/atomicfile`](../app/internal/sys/atomicfile) | Durable single-file replacement across OSes. | [Native integrations](native-integrations.md). |
| [`sys/appremoval`](../app/internal/sys/appremoval) | Locating and removing the installed desktop app (macOS Trash, Windows uninstaller), other-instance and other-account checks. | [Cleanup](cleanup-and-uninstall.md#removing-the-desktop-app). |
| [`sys/applog`](../app/internal/sys/applog) | Diagnostic sink, native log location, bounded rotation. | [Native integrations](native-integrations.md). |
| [`sys/elevate`](../app/internal/sys/elevate) | Interpreter quoting and batching privileged native steps. | [Native integrations](native-integrations.md). |
| [`sys/hosts`](../app/internal/sys/hosts) | Owned loopback hostname entries and verified removal. | [Native integrations](native-integrations.md). |
| [`sys/trust`](../app/internal/sys/trust) | Root-CA trust, removal, and native client certificate bootstrap. | [Native integrations](native-integrations.md). |
| [`sys/mdns`](../app/internal/sys/mdns) | LAN address selection, name advertisement, response probing. | [Native integrations](native-integrations.md). |
| [`sys/netfix`](../app/internal/sys/netfix) | Windows network profiles and application-owned firewall rules. | [Native integrations](native-integrations.md). |
| [`sys/autostart`](../app/internal/sys/autostart) | Platform login-startup records. | [Native integrations](native-integrations.md). |
| [`sys/reboot`](../app/internal/sys/reboot) | Restart requirement and restart request. | [Native integrations](native-integrations.md). |

### Smaller domain source files

The larger subsystem guides contain their own file tables. These smaller packages are fully accounted for here:

| Source | Role |
| --- | --- |
| [`plugins/plugins.go`](../app/internal/plugins/plugins.go) | `Plugin`, `Manager`, `Prepare`, `Catalog`, `FrontendRows`, dotenv/JSON read, safe variable replacement. |
| [`plugins/catalog.yml`](../app/internal/plugins/catalog.yml) | Embedded list of plugins offered in the panel. |
| [`plugins/plugins_test.go`](../app/internal/plugins/plugins_test.go) | Literal values, legacy migration, backend/frontend split, validation, catalog refresh, frontend rows. |
| [`release/pins.go`](../app/internal/release/pins.go) | `Pins`, required manifest fields, version/source-ref validation, diagnostic summary. |
| [`release/pins_test.go`](../app/internal/release/pins_test.go) | Version agreement, release-versus-development refs, commit-ID validation. |
| [`release/workflow_test.go`](../app/internal/release/workflow_test.go) | Executes the workflow's Node identity validator with synthetic inputs; requires Node. |

## Desktop files that form the backend contract

```text
app/frontend/
|-- package.json
|-- package-lock.json
|-- tsconfig.json
|-- vite.config.ts
|-- scripts/
|   |-- stage-install.mjs
|   |-- check-bindings.mjs
|   `-- prune-fonts.mjs
`-- src/
    |-- wails.d.ts
    |-- types.ts
    |-- lib/
    |   |-- bridge.ts
    |   |-- env-file.ts
    |   `-- run-steps.ts
    |-- state/
    |   `-- care-store.tsx
    |-- screens/
    |   `-- remove-screen.tsx
    `-- screens/panel/
        |-- advanced-tab.tsx
        |-- update-panel.tsx
        |-- env-editor.tsx
        |-- env-schema.ts
        |-- env-controls.tsx
        `-- plugin-table.tsx
```

| Contract file | Why a backend maintainer needs it |
| --- | --- |
| [`wails.d.ts`](../app/frontend/src/wails.d.ts) | Hand-maintained method names, argument lists, and promise result declarations. |
| [`types.ts`](../app/frontend/src/types.ts) | JSON-facing return shapes used by the desktop. |
| [`bridge.ts`](../app/frontend/src/lib/bridge.ts) | Lazy runtime lookup, call dispatch, event subscriptions, host logging. |
| [`care-store.tsx`](../app/frontend/src/state/care-store.tsx) | Async-job completion handling, boot/start request, status polling, restore-pending behavior. |
| [`run-steps.ts`](../app/frontend/src/lib/run-steps.ts) | Progress milestones derived from backend log messages. |
| [`advanced-tab.tsx`](../app/frontend/src/screens/panel/advanced-tab.tsx) | Collects the local admin password and passes it to protected settings/plugin/removal requests. |
| [`remove-screen.tsx`](../app/frontend/src/screens/remove-screen.tsx) | The screen the Windows uninstaller opens: server uninstall, client disconnect or unfinished-setup cleanup, then exit. |
| [`update-panel.tsx`](../app/frontend/src/screens/panel/update-panel.tsx) | The password-free Updates tab: CARE branch status and the desktop release updater. |
| [`env-editor.tsx`](../app/frontend/src/screens/panel/env-editor.tsx) | Concurrent environment reads, draft changes, writes and apply action. |
| [`env-file.ts`](../app/frontend/src/lib/env-file.ts) | Line-based environment parsing, value quoting, change application. |
| [`env-schema.ts`](../app/frontend/src/screens/panel/env-schema.ts) | Key/file ownership, friendly field constraints, managed-value notes. |
| [`env-controls.tsx`](../app/frontend/src/screens/panel/env-controls.tsx) | Raw string/undefined values rendered as typed controls. |
| [`plugin-table.tsx`](../app/frontend/src/screens/panel/plugin-table.tsx) | Password-free Plugins tab: catalog picker, custom editor, and save followed by `apply-plugins`. |

See [Wails API](wails-application.md) and [configuration](configuration-and-settings.md) for behavior rather than visual layout.

## Deployment kit

| Source | Runtime purpose |
| --- | --- |
| [`deployments/.env`](../deployments/.env) | Release version, image names/bases, upstream repos and refs. |
| [`docker-compose.yml`](../deployments/docker-compose.yml) | Services, dependencies, mounts, health checks, fixed project and network identity. |
| [`backend.env`](../deployments/backend.env) | Initial CARE/backend/backup-related settings; installed copy becomes clinic-specific. |
| [`frontend.env`](../deployments/frontend.env) | Initial CARE frontend build settings; installed copy is preserved. |
| [`Caddyfile`](../deployments/Caddyfile) | HTTPS/local CA, reverse proxy, storage routes, public root bootstrap, WAF behavior. |
| [`caddy.Dockerfile`](../deployments/caddy.Dockerfile) | Builds the proxy with the Coraza module. |
| [`backup.Dockerfile`](../deployments/backup.Dockerfile) | Tools and permissions required by backup/restore helper containers. |
| [`scripts/backup.sh`](../deployments/scripts/backup.sh) | Scheduled/manual backup modes, encryption, writer locking, publication and retention. |
| [`minio/entrypoint.sh`](../deployments/minio/entrypoint.sh) | Starts Silo, waits for readiness, configures credentials and buckets under the retained storage-service identity. |

Generated public trust files and restore/build working files belong to the installed copy, not the source kit.

## Build and automation

| File | Responsibility |
| --- | --- |
| [`app/go.mod`](../app/go.mod), [`app/go.sum`](../app/go.sum) | Go module identity, toolchain requirement, and dependencies. |
| [`app/wails.json`](../app/wails.json) | Desktop/frontend build hooks, output identity, installer metadata. |
| [`app/.golangci.yml`](../app/.golangci.yml) | Go lint and formatter policy. |
| [`app/.gitignore`](../app/.gitignore) | Separates generated/staged output from versioned source. |
| [`frontend/package.json`](../app/frontend/package.json), [`package-lock.json`](../app/frontend/package-lock.json) | Desktop frontend scripts and dependency lock. |
| [`frontend/tsconfig.json`](../app/frontend/tsconfig.json) | Type-checking and local import aliases. |
| [`frontend/vite.config.ts`](../app/frontend/vite.config.ts) | Embedded-compatible asset URLs, output directory, placeholder retention. |
| [`stage-install.mjs`](../app/frontend/scripts/stage-install.mjs) | Refreshes the embedded-kit staging tree from `deployments/`. |
| [`check-bindings.mjs`](../app/frontend/scripts/check-bindings.mjs) | Checks desktop declarations against exported Go methods and arities. |
| [`prune-fonts.mjs`](../app/frontend/scripts/prune-fonts.mjs) | Removes specified legacy asset extensions after bundling. |
| [`build/darwin/Info.plist`](../app/build/darwin/Info.plist) | macOS production bundle metadata template. |
| [`build/darwin/Info.dev.plist`](../app/build/darwin/Info.dev.plist) | Development bundle metadata, including local-network transport allowance. |
| [`build/appicon.png`](../app/build/appicon.png) | Wails application-icon source. |
| [`build/windows/info.json`](../app/build/windows/info.json), [`wails.exe.manifest`](../app/build/windows/wails.exe.manifest) | Windows version resource and manifest templates filled from `wails.json`. |
| [`build/windows/installer/project.nsi`](../app/build/windows/installer/project.nsi) | NSIS installer definition; `wails_tools.nsh` beside it is generated. |
| [`ci.yml`](../.github/workflows/ci.yml) | Compilation, package boundary, test, formatting/lint, frontend build checks. |
| [`release.yml`](../.github/workflows/release.yml) | Release identity, platform builds, packaging/signing, draft release or artifact upload. |
| [`actions/install-nsis`](../.github/actions/install-nsis/action.yml) | Pinned NSIS install shared by CI and release. |
| [`signpath/artifact-configuration.xml`](../.github/signpath/artifact-configuration.xml) | SignPath artifact configuration: metadata restrictions the signed files must satisfy. |

The full pipeline and current platform/tool versions are in [development and release](development-and-release.md).

## Navigating a change bottom-up

For a native behavior, start with its `internal/sys` package, then the calling domain/`Clinic` code, then the `App` boundary and desktop consumer. For a button-triggered behavior, follow the same path in reverse.

When source files are split or moved, keep this inventory and the owning subsystem guide synchronized. A new helper is not fully integrated until its error path, ownership, caller, and test seam are understandable from those two places.
