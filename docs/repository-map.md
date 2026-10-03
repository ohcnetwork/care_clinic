# Repository and file map

[Documentation index](README.md)

This map covers the application boundary, major internal packages, desktop
workflows and their build/test contracts. The package trees highlight principal
files rather than exhaustively listing generated output. Detailed subsystem
inventories remain in the linked guides.

## Repository-level structure

```text
care_desktop/
|-- README.md
|-- docs/
|   |-- README.md
|   |-- desktop-workflows.md
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
|   |-- releases.md
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
| [`app_confirmation.go`](../app/app_confirmation.go) | ID-scoped permission requests, independent response channel, native fallback and shutdown cancellation. | [Confirmation protocol](wails-application.md#in-window-permission-confirmations). |
| [`app_quit.go`](../app/app_quit.go) | Registered running-job quit requests and guarded responses. | [Closing the application](wails-application.md#closing-the-application). |
| [`app_config.go`](../app/app_config.go) | `Config`, OS configuration path, strict initial loading, cached/atomic saves and forgetting state. | [Configuration](configuration-and-settings.md). |
| [`app_client.go`](../app/app_client.go) | Native client connection, pinned trust/ownership journal, TLS verification, and exact-certificate uninstall with role reset after success. | [Native integrations](native-integrations.md), [Wails application](wails-application.md). |
| [`app_installdir.go`](../app/app_installdir.go) | Fixed runtime kit path, unpacking/preservation rules, creation of a configured `Clinic`. | [Configuration](configuration-and-settings.md). |
| [`app_actions.go`](../app/app_actions.go) | Read/write job gates, async runner, lifecycle/admin guards, setup, action dispatch, failed-install cleanup. | [Wails application](wails-application.md), [clinic lifecycle](clinic-lifecycle.md). |
| [`app_setup_check.go`](../app/app_setup_check.go) | Review preflight and the same checks repeated under RunSetup's exclusive lock. | [Setup configuration order](configuration-and-settings.md#setup-configuration-order). |
| [`app_recovery.go`](../app/app_recovery.go) | Recovery-key/code export and verification, replacement, Desktop password changes and offline reset. | [Recovery materials](backups-and-restore.md#3-backup-recovery-file-and-desktop-admin-recovery). |
| [`app_setup_retry.go`](../app/app_setup_retry.go) | Retained setup attempt, failure metadata, non-destructive retry validation and preparation/startup boundary. | [Installation retries](desktop-workflows.md#during-installation). |
| [`app_update.go`](../app/app_update.go) | CARE branch-update status/check/dismiss, the background update watcher, and the GitHub-release desktop updater. | [Clinic lifecycle](clinic-lifecycle.md), [releases](releases.md). |
| [`app_selfupdate.go`](../app/app_selfupdate.go) | macOS bundle verification/replacement and post-exit reopening. | [Wails application](wails-application.md). |
| [`app_status.go`](../app/app_status.go) | State/health/tool/network queries, provisioning controls, name/password/folder validation, pre-setup naming. | [Wails application](wails-application.md), [native integrations](native-integrations.md). |
| [`app_ui.go`](../app/app_ui.go) | Native URL/folder/log actions and login-startup controls. | [Wails application](wails-application.md). |
| [`app_env.go`](../app/app_env.go) | Authorized reads and atomic writes of the two installed environment files. | [Configuration](configuration-and-settings.md). |
| [`app_plugins.go`](../app/app_plugins.go) | Lifecycle-guarded, password-free plugin access through the domain manager. | [Plugins](plugins.md). |
| [`app_backup.go`](../app/app_backup.go) | Policy, listing/import inspection, restore dispatch, backup picker, and backup-directory changes. | [Backups](backups-and-restore.md), [Wails application](wails-application.md). |
| [`app_storage.go`](../app/app_storage.go) | Disk/backup-space assessments, cached reports, background monitoring and storage events. | [Storage monitoring](native-integrations.md#disk-space-and-storage-monitoring). |
| [`app_uninstall.go`](../app/app_uninstall.go) | Authorized normal uninstall, removal checkpoint, post-cleanup scan, local-state cleanup and event. | [Cleanup](cleanup-and-uninstall.md). |
| [`app_osremove.go`](../app/app_osremove.go) | `--uninstall`/`--uninstall-check` modes for the Windows uninstaller, the shared "still set up" check, and in-app removal of the desktop app. | [Cleanup](cleanup-and-uninstall.md#removing-the-desktop-app). |
| [`app_residue.go`](../app/app_residue.go) | Residue scanning, recovery of old install location, confirmed purge, preserving the selected first-run name. | [Cleanup](cleanup-and-uninstall.md). |
| [`password.go`](../app/password.go) | Shared setup password policy. | [Wails application](wails-application.md). |
| [`app_env_test.go`](../app/app_env_test.go) | Synthetic-app regressions for concurrent settings reads, guards, conflicting writes, and saved retention values. | [Configuration](configuration-and-settings.md). |

### Application-boundary regression groups

| Tests | Contract |
| --- | --- |
| [`app_confirmation_test.go`](../app/app_confirmation_test.go), [`app_quit_contract_test.go`](../app/app_quit_contract_test.go), [`app_quit_test.go`](../app/app_quit_test.go), [`app_ui_test.go`](../app/app_ui_test.go) | Consent/quit snapshots, stale responses, cancellation, independent locking and native question handling. |
| [`app_onboarding_contract_test.go`](../app/app_onboarding_contract_test.go), [`app_setup_check_test.go`](../app/app_setup_check_test.go), [`app_role_test.go`](../app/app_role_test.go) | Role persistence and clearing, prerequisite/setup rejection, worker completion and retry boundaries. |
| [`app_setup_retry_test.go`](../app/app_setup_retry_test.go) | Retained-attempt validation, unchanged settings/recovery requirements, preparation reuse and retry failure classification. |
| [`app_desktop_windows_test.go`](../app/app_desktop_windows_test.go), [`internal/clinic/desktop_windows_test.go`](../app/internal/clinic/desktop_windows_test.go) | Recovery dialog defaults and default backup paths match Windows' Desktop known folder; explicit backup paths remain unchanged. |
| [`internal/sys/proc/launcher_test.go`](../app/internal/sys/proc/launcher_test.go) | Launcher exit, failure, timeout/cancellation, and high-volume descendant output that remains writable after both the launcher and its parent exit, without creating output files. |
| [`app_recovery_test.go`](../app/app_recovery_test.go) | Recovery exports, password reset/replacement, filesystem guards and persisted recovery state. |
| [`app_client_test.go`](../app/app_client_test.go), [`app_mdns_test.go`](../app/app_mdns_test.go) | Native client contracts and server name-advertisement checks. |
| [`app_update_test.go`](../app/app_update_test.go), [`app_update_access_test.go`](../app/app_update_access_test.go) | Desktop release/update behavior and access outside an installed server. |
| [`app_env_test.go`](../app/app_env_test.go), [`app_plugins_test.go`](../app/app_plugins_test.go) | Settings/plugin persistence, access and operation gates. |
| [`app_installdir_test.go`](../app/app_installdir_test.go), [`app_osremove_test.go`](../app/app_osremove_test.go), [`app_log_test.go`](../app/app_log_test.go) | Kit embedding, executable-removal checks and native error logging. |

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
|-- storage/
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
    |-- diskspace/
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
| [`internal/storage`](../app/internal/storage) | Storage thresholds, backup-space estimates, policy/status reporting and backup-run state. | [Storage monitoring](native-integrations.md#disk-space-and-storage-monitoring). |
| [`sys/diskspace`](../app/internal/sys/diskspace) | Platform filesystem free-space measurements. | [Storage monitoring](native-integrations.md#disk-space-and-storage-monitoring). |
| [`sys/proc`](../app/internal/sys/proc) | Child-process creation, streamed-command network failure classification, pipe-free launcher execution with discarded output, Windows Desktop known-folder lookup, and PATH repair. | [Native integrations](native-integrations.md). |
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
|-- playwright.config.ts
|-- scripts/
|   |-- stage-install.mjs
|   |-- check-bindings.mjs
|   `-- prune-fonts.mjs
|-- tests/
|   |-- fixtures/
|   |   |-- index.html
|   |   `-- host.ts
|   `-- *.spec.ts
`-- src/
    |-- App.tsx
    |-- wails.d.ts
    |-- types.ts
    |-- lib/
    |   |-- bridge.ts
    |   |-- env-file.ts
    |   `-- run-steps.ts
    |-- state/
    |   `-- care-store.tsx
    |-- components/
    |   |-- confirmation-dialog.tsx
    |   |-- quit-dialog.tsx
    |   |-- start-update-card.tsx
    |   `-- onboarding.tsx and onboarding.css
    |-- hooks/
    |   `-- use-app-update.ts
    `-- screens/
        |-- role-screen.tsx
        |-- client-screen.tsx
        |-- remove-screen.tsx
        |-- setup/
        |-- install/
        `-- panel/
```

| Contract file | Why a backend maintainer needs it |
| --- | --- |
| [`wails.d.ts`](../app/frontend/src/wails.d.ts) | Hand-maintained method names, argument lists, and promise result declarations. |
| [`types.ts`](../app/frontend/src/types.ts) | JSON-facing return shapes used by the desktop. |
| [`bridge.ts`](../app/frontend/src/lib/bridge.ts) | Lazy runtime lookup, call dispatch, event subscriptions, host logging. |
| [`care-store.tsx`](../app/frontend/src/state/care-store.tsx) | Async-job completion handling, boot/start request, status polling, restore-pending behavior. |
| [`run-steps.ts`](../app/frontend/src/lib/run-steps.ts) | Progress milestones derived from backend log messages. |
| [`App.tsx`](../app/frontend/src/App.tsx) | Root flow routing and always-mounted permission/quit dialogs. |
| [`role-screen.tsx`](../app/frontend/src/screens/role-screen.tsx), [`client-screen.tsx`](../app/frontend/src/screens/client-screen.tsx) | First-run navigation without persistence, client discovery/connection/recovery and saved connection state. |
| [`setup-screen.tsx`](../app/frontend/src/screens/setup/setup-screen.tsx), [`use-setup-checks.ts`](../app/frontend/src/screens/setup/use-setup-checks.ts) | Role initialization, platform-aware checks, configuration/recovery steps and Review preflight. |
| [`installing-screen.tsx`](../app/frontend/src/screens/install/installing-screen.tsx), [`failed-screen.tsx`](../app/frontend/src/screens/install/failed-screen.tsx) | Log-backed milestones, quiet-activity warning, retained-attempt retry and explicit cleanup fallback. |
| [`confirmation-dialog.tsx`](../app/frontend/src/components/confirmation-dialog.tsx), [`quit-dialog.tsx`](../app/frontend/src/components/quit-dialog.tsx) | Request registration, stale-response protection, safe consent focus and response errors. |
| [`use-app-update.ts`](../app/frontend/src/hooks/use-app-update.ts), [`panel-update-lock.tsx`](../app/frontend/src/screens/panel/panel-update-lock.tsx) | Shared updater, installer acknowledgement, restart guard and live/stale-operation exclusion. |
| [`panel-screen.tsx`](../app/frontend/src/screens/panel/panel-screen.tsx), [`panel-requirements.tsx`](../app/frontend/src/screens/panel/panel-requirements.tsx) | Panel navigation, shared task guards and actionable native requirements. |
| [`overview-tab.tsx`](../app/frontend/src/screens/panel/overview-tab.tsx), [`storage-tab.tsx`](../app/frontend/src/screens/panel/storage-tab.tsx), [`phone-dialog.tsx`](../app/frontend/src/screens/panel/phone-dialog.tsx) | Health/actions, storage measurements and the mobile setup QR. |
| [`backups-tab.tsx`](../app/frontend/src/screens/panel/backups-tab.tsx), [`restore-backup-dialog.tsx`](../app/frontend/src/screens/panel/restore-backup-dialog.tsx) | Actual policy/destination, backup operations, restore consent and cancelled/stale preflight handling. |
| [`advanced-tab.tsx`](../app/frontend/src/screens/panel/advanced-tab.tsx), [`admin-recovery.tsx`](../app/frontend/src/screens/panel/admin-recovery.tsx) | Fixed 15-minute UI unlock, sensitive-state clearing, password/recovery controls and protected administration. |
| [`remove-screen.tsx`](../app/frontend/src/screens/remove-screen.tsx) | The screen the Windows uninstaller opens: server uninstall, client disconnect or unfinished-setup cleanup, then exit. |
| [`update-panel.tsx`](../app/frontend/src/screens/panel/update-panel.tsx) | The password-free Updates tab: CARE branch status and the desktop release updater. |
| [`env-editor.tsx`](../app/frontend/src/screens/panel/env-editor.tsx) | Concurrent environment reads, draft changes, writes and apply action. |
| [`env-file.ts`](../app/frontend/src/lib/env-file.ts) | Line-based environment parsing, value quoting, change application. |
| [`env-schema.ts`](../app/frontend/src/screens/panel/env-schema.ts) | Key/file ownership, friendly field constraints, managed-value notes. |
| [`env-controls.tsx`](../app/frontend/src/screens/panel/env-controls.tsx) | Raw string/undefined values rendered as typed controls. |
| [`advanced-env.ts`](../app/frontend/src/screens/panel/advanced-env.ts) | Friendly validation and changed-key merges that preserve unedited file bytes. |
| [`plugin-table.tsx`](../app/frontend/src/screens/panel/plugin-table.tsx) | Password-free Plugins tab: catalog picker, custom editor, and save followed by `apply-plugins`. |
| [`plugin-model.ts`](../app/frontend/src/screens/panel/plugin-model.ts) | Catalog reconciliation, typed settings preservation and frontend/native-compatible validation. |
| [`tests/fixtures/host.ts`](../app/frontend/tests/fixtures/host.ts) | Simulated native state/events used only by the explicit Vite test entry. |

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
| [`frontend/playwright.config.ts`](../app/frontend/playwright.config.ts) | Isolated loopback test server, test-only entry, browser workers and failure traces. |
| [`.pre-commit-config.yaml`](../.pre-commit-config.yaml) | Whitespace/YAML checks, Go formatting and package-wide Go lint before commits. |
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
