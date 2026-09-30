# CARE Desktop backend documentation

CARE Desktop is a local control application for a clinic's CARE installation. Its Go backend installs and operates a Docker Compose stack, maintains the computer's local networking, and manages encrypted backups. Wails connects that backend to the desktop control panel.

At first run, choose a persisted **Server** role to host the clinic or **Client**
to connect to its `.local` address using native certificate setup and automatic
HTTPS verification. The server operations documented here do not run on clients.

This documentation explains the current implementation, from operating-system primitives to the methods the desktop interface calls. It includes the deployment kit and the small part of the desktop frontend that defines the backend contract. It does not attempt to document the CARE medical application, its Django API, or React presentation components.

## Start here

| Guide | What you will understand |
| --- | --- |
| [Architecture and design choices](architecture.md) | The system boundary, layers, state, dependency direction, and the reasons behind the design. |
| [Repository and file map](repository-map.md) | Where every Go component lives, what each file does, and which guide explains it. |
| [Wails application and API](wails-application.md) | Process startup, background jobs, concurrency, authorization, all bound methods, and events. |
| [Configuration and settings](configuration-and-settings.md) | The installed directory, persisted configuration, environment files, plugins, and settings changes. |
| [Plugins](plugins.md) | How CARE loads backend and frontend plugins, the `catalog.yml` format, and how Desktop installs and syncs them. |
| [Clinic lifecycle and deployment](clinic-lifecycle.md) | Setup, image builds, starting, migrations, service relationships, and stopping. |
| [Backups and restore](backups-and-restore.md) | Encryption, key preservation, scheduling, retention, staged restore, and interruption recovery. |
| [Cleanup and uninstall](cleanup-and-uninstall.md) | What each removal operation deletes or preserves, ownership checks, and partial-cleanup recovery. |
| [Native integrations](native-integrations.md) | Process execution, file persistence, logs, prerequisites, elevation, TLS trust, mDNS, and OS differences. |
| [Development and release](development-and-release.md) | Local builds, embedded assets, release pins, CI, packaging, and changing the backend safely. |
| [Releasing CARE Desktop](releases.md) | Preparing a version, how merging a version bump starts the release, reviewing and publishing the draft, signing limitations, and retrying safely. |
| [Facility setup page](seed-data.md) | The browser wizard at `/seed-data` that loads a new clinic's first data through CARE's API, and the master-sheet converter behind it. |

For a first reading, follow the table from top to bottom. If you are fixing one behavior, use the task map below instead.

## Find a behavior

| Task or question | Read |
| --- | --- |
| "What actually runs on the clinic computer?" | [System architecture](architecture.md) and [deployment services](clinic-lifecycle.md). |
| "Where does a button click enter Go?" | [Wails application and API](wails-application.md). |
| "Why does an operation say something else is running?" | [Concurrency and job protocol](wails-application.md#concurrency-and-job-protocol). |
| "Which `backend.env` does the editor read?" | [Configuration and settings](configuration-and-settings.md). |
| "Why did a setting require a rebuild?" | [Applying settings](configuration-and-settings.md#applying-settings) and [the different builds](development-and-release.md#the-different-builds). |
| "How are patient data and uploaded files stored?" | [Clinic lifecycle](clinic-lifecycle.md) and [backups](backups-and-restore.md). |
| "What happens if restore or uninstall is interrupted?" | [Backups and restore](backups-and-restore.md) and [cleanup and uninstall](cleanup-and-uninstall.md). |
| "How do I add a plugin to the catalog?" | [Plugins](plugins.md#adding-a-catalog-entry). |
| "The app works here but not on another device." | [Native integrations](native-integrations.md). |
| "How do staff computers connect without Docker or Git?" | [Native client setup and initial trust](native-integrations.md#native-client-setup-and-trust-on-first-use). |
| "This client previously hosted CARE and now cannot reach another server." | [Client recovery and earlier-install cleanup](client-recovery.md). |
| "How do I reproduce the installed version?" | [Release identity](development-and-release.md#release-identity-and-pins). |
| "How do I release a new version?" | [Release runbook](releases.md). |
| "How does a new clinic get its facility, staff and master data?" | [Facility setup page](seed-data.md). |
| "Which file should I change?" | [Repository map](repository-map.md). |

## Vocabulary

| Term | Meaning in this repository |
| --- | --- |
| CARE Desktop | The Go/Wails executable and its embedded desktop control panel. |
| Desktop frontend | `app/frontend/`: the interface inside the Wails window. It is not the CARE web application. |
| CARE frontend | The separately built `care_fe` web application served to clinic staff by the Compose stack. |
| `App` | The Wails-facing object in `app/*.go`; owns application state, authorization, jobs, and native dialogs. |
| `Clinic` | The orchestration object in `app/internal/clinic/`; operates the installed stack without importing Wails. |
| Deployment kit | The files in `deployments/`, embedded into the desktop executable and unpacked for Docker Compose. |
| Installed kit | The runtime copy of that kit on the clinic computer; distinct from source and build staging. |
| Compose project | The named set of containers, networks, and volumes belonging to `care-desktop`. |
| Named volume | Docker-managed persistent storage, separate from the desktop executable and source checkout. |
| Pin | A version, image reference, repository URL, or source commit describing what a desktop release expects. |
| Sidecar | A supporting container; here, notably the container running scheduled backups. |
| mDNS | Multicast DNS, used to advertise the clinic's `.local` name to nearby devices. |
| Root CA | The local certificate authority used to establish trust in the clinic's HTTPS certificate. |
| Recovery key | The password-encrypted private key required to decrypt encrypted backups; it is not the password itself. |
| Restore journal | Durable metadata used to recognize and recover an interrupted staged restore. |
| Residue | Resources left by an earlier or partially removed CARE Desktop installation. |

## How to read the diagrams

Diagrams are Mermaid code blocks inside Markdown, so there are no separate image files to keep synchronized. GitHub renders them directly. In an editor without Mermaid support, the node labels and arrows remain readable as text.

An arrow in an architecture diagram means "calls or depends on" unless stated otherwise. A flowchart describes ordering and decisions, not parallel execution. Sequence diagrams explicitly distinguish a method being accepted from its eventual completion.

## Scope and source of truth

These guides describe the code in this checkout, including the shared read lock for environment/plugin reads, staged restore recovery, recovery-key-safe cleanup, and the Silo-backed storage service.

The root [design notes](../design.md) explain the original intent, but include historical file counts, signatures, and flows. The code and these current guides take precedence where they differ. For example, settings reads are no longer exclusive jobs, backups use an exported recovery file instead of a password, and restore is no longer a direct drop-and-reload of the live database.

File links lead to the implementation rather than fixed line numbers. When changing a component, update its guide, affected flowcharts, and the API or file map if the contract changed. Do not treat a diagram as a substitute for error handling in the code.

## Operational caution

Running the desktop application is not a read-only demonstration: it can refresh an existing installed kit, start the clinic, and advertise its name. Setup, restore, networking repair, and removal have real effects on the current computer. Use a disposable development environment for end-to-end experiments.

The presence of regression tests is not a claim that every operating system, power-loss scenario, or production backup has been exercised. The subsystem guides distinguish implemented recovery behavior from what their tests actually cover.
