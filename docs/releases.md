# Releasing CARE Clinic

[Documentation index](README.md)

A release starts on its own when a change to `CARE_CLINIC_VERSION` is merged
into `main`, and can also be started by hand from GitHub Actions. Do not create
or push a tag to start a build. The workflow reads the selected commit's
[`deployments/.env`](../deployments/.env), runs CI, packages the CI-built
applications, creates a tag automatically, and creates a **draft prerelease**.
Nothing is published to clinic users automatically.

Windows builds are signed through SignPath when the
[SignPath configuration](#windows-signing-signpath) is present; otherwise they
are unsigned previews. macOS retains the existing optional Developer ID signing
and notarization flow, using the same secrets listed below. Without those
credentials it retains Wails' ad-hoc signature only. The release manifest records
signing status separately for each platform. Do not tell users to disable OS
protection.

The CARE engine is built and published separately from the desktop app. To change
the engine version, run the manually started **CARE engine** workflow and copy
its version and checksums into `deployments/.env`; the desktop release workflow
verifies the pinned engine and platform downloads before packaging. See the
[CARE engine guide](vm.md) for the process.

## Prepare a version

### Personal testing versus the general release workflow

Current development publishes to `praffq/care_clinic`, not the organization
repository. The latest personal desktop release is `v0.1.15`; its published
assets are an Apple-silicon macOS DMG and `SHA256SUMS`. Windows runtime/packaging
and an Intel macOS installation have not been validated in this test cycle.
The fork's automated desktop release is still blocked by the missing
`qemu-10.1.0-windows-x86_64.zip` asset on the pinned engine release. A
checksum-pinned, reproducible package is prepared locally; native Windows
validation and support-asset publication remain pending. The package script
and validation instructions are in [the engine guide](vm.md#packaging-qemu-for-windows).
The Mac
test releases have therefore been built, ad-hoc signed, packaged and published
manually after CI. This does not establish a working all-platform release
pipeline or Developer ID notarization.

Engine publication and CARE image/line publication are separate. The `3.1`
branch test was published through image CI without a new desktop app release;
Clinic `0.1.15` already supports it. See [images and lines](images-and-lines.md).

1. Create a PR updating `deployments/.env`.
2. Set `CARE_CLINIC_VERSION` to a new numeric version, for example `0.1.1`.
   Use a new version for embedded application or deployment-kit changes.
   CARE images and line metadata published independently do not require a
   desktop version bump when existing apps already support them.
3. Update the dependency pins needed by that release.
4. Review the PR, let CI pass, and merge it into `main`. The merge starts the
   release; see [Build the release](#build-the-release).

| Configuration | What to maintain |
| --- | --- |
| `CARE_CLINIC_VERSION` | One `X.Y.Z` value. `-dev` builds are not accepted by the release workflow. |
| `CARE_LINES_URL`, `CARE_DEFAULT_LINE` | HTTPS release-line manifest URL and the default line for new clinics. CARE runtime/builder digests are maintained in the separate `lines.json` manifest. |
| `CADDY_WAF_PREBUILT`, `BACKUP_PREBUILT` | Optional digest-pinned proxy/backup images. Blank values retain local builds. |
| `POSTGRES_IMAGE`, `REDIS_IMAGE`, `MINIO_IMAGE`, `CADDY_IMAGE` | Deliberately chosen image versions; use immutable digests when available. |
| `CORAZA_VERSION` | The WAF module version used to build the proxy. |
| `BACKUP_IMAGE`, `CADDY_WAF_IMAGE`, `BACKEND_IMAGE`, `FRONTEND_IMAGE` | Local output image names. These are not upstream version selectors; normally leave them alone. |
| `CARE_VM_*`, `DOCKER_CLI_*`, `COMPOSE_MACOS_*`, `COMPOSE_WINDOWS_*`, `QEMU_WINDOWS_*`, `DOCKER_LINUX_*`, `COMPOSE_LINUX_*` | Version and SHA-256 pins for the CARE engine, host Docker/Compose clients, Windows QEMU, and native Linux Docker/Compose downloads. See [below](#bump-a-pinned-prerequisite) and the [CARE engine guide](vm.md). |

Publish verified fixes to each maintained release line. Existing clinics retain
their line in `line.json`; changing the default affects new installs, not the
line an existing clinic follows. Newer lines require explicit upgrades. See
[images and release lines](images-and-lines.md) for image publication and adding
a line; that CI workflow is separate from desktop packaging.

Do not add passwords, signing credentials, or clinic-specific data to this file:
the exact file is embedded in the app and attached to the release.

### Bump a pinned prerequisite

The desktop release pins are maintained in `deployments/.env`. For the CARE
engine, first build and publish a version with the manually run
[CARE engine workflow](vm.md#building-the-engine), then update the corresponding
`CARE_VM_*` version and checksum values. Update the `DOCKER_CLI_*`,
`COMPOSE_MACOS_*`, `COMPOSE_WINDOWS_*`, and `QEMU_WINDOWS_*` pins when their
host-side tools change. Linux continues to use `DOCKER_LINUX_*` and
`COMPOSE_LINUX_*` static-binary pins. The release workflow verifies these
pinned files before it packages the app.

`deployments/.env` is the version source of truth. Do not maintain
`app/wails.json`'s `info.productVersion` independently. Run
`node app/frontend/scripts/stage-install.mjs` after changing the version and
commit the synchronized `app/wails.json`. Invoke staging before Wails itself:
Wails reads metadata before its pre-build hook, so relying on that hook alone
can package a stale version.

## Build the release

Merging the version PR into `main` runs **Release CARE Clinic**. Its first
job, **Decide whether to release**, compares `CARE_CLINIC_VERSION` with the
commit before the push: when it changed, the release continues; when only other
`.env` values changed (an image or prerequisite pin, a CARE line setting), every
other job is skipped and the run ends without a release. Pushes that do not
touch `deployments/.env` do not start the workflow at all.

1. Open **Actions -> Release CARE Clinic** and find the run for the merge.
2. Wait for every job to finish.
3. Open the resulting draft in the repository's **Releases** page.

To start a release by hand, for example to retry after a failure that needed a
new commit, select **Run workflow**, choose the approved branch (normally
`main`), and start the run. A manual run always releases; there is no version
form, the version comes from `.env`. The workflow must be present on the default
branch for the manual button to appear. Maintainers need permission to run
workflows. GitHub CLI equivalent:

```sh
gh workflow run release.yml --ref main
```

All jobs use the workflow run's selected source commit, even if the branch moves
while the build is running. A matching tag is created only after packaging
succeeds. For `CARE_CLINIC_VERSION=0.1.1`, the tag is `v0.1.1`.

## What the workflow does

| Stage | Behavior |
| --- | --- |
| Gate | On a push to `main`, continue only when `CARE_CLINIC_VERSION` differs from the previous commit; otherwise skip every job. A manual run always continues. |
| Validate | Reject malformed/duplicate version values and an existing release version. |
| Verify prerequisites | Download the pinned CARE engine and platform tools, then compare SHA-256 values before building. Linux Docker Engine and Compose pins are checked as well. |
| Check and build | Call the same CI workflow used by PRs: lint, race tests, frontend checks, and real macOS/Windows native builds. No separate untested release rebuild. |
| Windows signing | With the SignPath configuration, submit the CI-built `CARE Clinic.exe` and `uninstall.exe` for signing in one request, rebuild the NSIS installer around the signed files with the installer inputs CI produced, and submit the installer for signing. Each request waits for an approver. Without the configuration, both jobs are skipped and the CI installer is used unsigned. |
| macOS signing | With the existing credentials, sign the app with hardened runtime, notarize/staple it, then sign and notarize/staple the DMG. Otherwise explicitly report ad-hoc signing. |
| Package | Wrap the CI macOS app in a DMG and copy the signed (or CI-built unsigned) Windows installer. Verify macOS metadata matches the release version. |
| Record | Save the exact release configuration, source commit, workflow run URL, signing status, and SHA-256 file checksums. |
| Draft | Verify the complete asset set, create/reuse the tag at the exact source commit, and create a draft marked as a prerelease. Never replace an existing release. Runs after an unsigned package too: its condition allows the Windows signing jobs to be skipped, which GitHub's default job condition would not. |

Release runs are serialized and do not cancel an active release. CI invoked by
a release has a separate concurrency group from ordinary PR/main CI.

For version `0.1.1`, the draft has these five assets:

```text
CARE-Clinic-0.1.1-macos.dmg
CARE-Clinic-0.1.1-windows-amd64-setup.exe
release-config.env
release-manifest.json
SHA256SUMS
```

The manifest's `source_commit` identifies the desktop source. The configuration
records the CARE FE/BE revisions and image pins. Checksums detect altered or
incomplete downloads; they are **not** publisher signatures or clinic TLS
certificate fingerprints.

Damaged downloads are rejected without anyone comparing checksums by hand:

- **First install on Windows:** `project.nsi` sets `CRCCheck force`, so the
  installer verifies itself before it runs. A truncated or corrupted copy stops
  with NSIS's "Installer integrity check has failed… obtain a new copy" message,
  and `/NCRC` cannot skip the check. A test fails if the setting is removed.
- **First install on macOS:** the UDZO disk image carries its own checksums, and
  macOS refuses to open a damaged one.
- **In-app updates:** the app checks the installer against `SHA256SUMS`, retries
  a bad download once, then deletes it and asks the operator to update again. See
  [app updates](wails-application.md).

The draft's notes include a "Verify your download" section with the
`Get-FileHash` and `shasum -a 256` commands for checking a file by hand.

The combined package is also retained as the `care-release-assets` workflow
artifact for 14 days. These temporary Actions artifacts are for maintainers;
the eventual published release assets are the durable downloads.

## Review and publish

Before publishing:

1. Check both installers and their checksums. Use a disposable clinic to check
   fresh setup, existing-clinic upgrade, browser access, and operation without
   internet after provisioning.
2. Add release notes describing visible changes, dependency changes, known
   limitations, and any database migration or prerequisite requirements.
3. For an existing clinic, require a verified backup before an update is
   applied. Preserve
   its CA identity and persistent data; an application upgrade should not
   require all client devices to reinstall certificate trust.
4. Decide how to publish. The draft is created as a prerelease. The in-app
   updater only offers GitHub's **latest** release, which never includes
   prereleases, so installed clinics are offered this version only if you
   untick **Set as a pre-release** and publish it as the latest release. Leave
   it ticked to share a build with testers without offering it to clinics.
5. If distributing the current preview to testers, publish it **as a
   prerelease**, retaining the unsigned-download warnings.

A draft is not public, and a prerelease is not the normal GitHub "latest stable"
download. Publishing a stable release is a separate maintainer decision.

Once published, do not move the tag or replace its files. Corrections require a
new version and another configuration PR. A database downgrade is not guaranteed
by reinstalling an older desktop binary; recovery may require a version-matched
backup.

## Failure and retry

| Failure | Action |
| --- | --- |
| Validation, CI, or packaging failed | Fix the issue. Rerun the failed job for a transient problem, or start a new run for the corrected commit. No tag/release is created before the draft job. |
| Version already has a release | Bump `.env` to a new version. Never delete a published release to reuse its version. |
| Existing tag points to another commit | Choose a new version. The workflow will not move the tag. |
| Tag created, but draft creation failed | Rerun from the same commit. A matching existing tag can be reused if no release exists. |
| Draft created, but asset upload failed | Inspect it. Delete only that incomplete, unpublished draft, retain its matching tag, and rerun from the same commit. Never publish a partial draft. |
| Tag creation rejected | Check repository tag rules and Actions permissions. Only the final job requests `contents: write`; configure repository rules to allow the intended release actor without allowing tag replacement. |

Do not restart a release from a different commit using an already reserved
version/tag. The selected commit and configuration are part of the release's
identity.

## Scope and current limitations

Desktop packaging does not deploy a running stack to clinics or bundle Docker
installers. CARE runtime and builder images are published separately; setup
needs the manifest, registry images, and any customization dependencies.
Digest-pinned images do not make local plugin/package downloads immutable.

Updates reach installed clinics by two separate routes, described in
[images and release lines](images-and-lines.md) and
[configuration and settings](configuration-and-settings.md):

- **CARE backend and frontend** receive same-line fixes through background
  manifest checks and staged image downloads. They apply when accepted or at the
  next start; no desktop release is needed. A newer line is only offered and
  requires an explicit upgrade after a safety backup.
- **CARE Clinic** is offered from published GitHub releases in **Updates** and
  is operator-initiated. The current line's cached app-version limits can block
  an incompatible desktop update.

## Windows signing (SignPath)

Windows releases are signed with a certificate issued to
[SignPath Foundation](https://signpath.org) under its free open-source program.
The obligations that come with it are published in the README's
[code signing policy](../README.md#code-signing-policy); keep that section
accurate when the team or the app's network behaviour changes.

### Configuration

| Setting | Kind | Purpose |
| --- | --- | --- |
| `SIGNPATH_API_TOKEN` | secret | API token of a SignPath user with the Submitter role. |
| `SIGNPATH_ORGANIZATION_ID` | variable | SignPath organization ID. |
| `SIGNPATH_PROJECT_SLUG` | variable | SignPath project slug. |
| `SIGNPATH_SIGNING_POLICY_SLUG` | variable | Slug of the release signing policy. |

Set all four or none. A partial configuration fails the release at validation
instead of silently producing unsigned installers.

### SignPath project

- Repository `https://github.com/ohcnetwork/care_desktop`, trusted build system
  GitHub.
- Artifact configuration:
  [`.github/signpath/artifact-configuration.xml`](../.github/signpath/artifact-configuration.xml).
  It signs the PE files inside a GitHub artifact (at most two) and rejects them
  unless product name, company name and product version match `app/wails.json`
  and the release version, which is the same check CI runs. Both requests use it.
- Release signing policy: origin verification on, allowed branch `main`, manual
  approval required. The workflow passes `version` as a parameter.

### During a release

1. **Sign Windows application** submits the CI-built `CARE Clinic.exe` and
   `uninstall.exe` and waits up to an hour for an approver.
2. **Build installer around the signed application** verifies both signatures,
   then runs makensis with the installer inputs CI produced, the signed application
   and `-DSIGNED_UNINSTALLER`, so the installer copies the signed uninstaller
   instead of generating an unsigned one at install time.
3. **Sign Windows installer** submits the installer; approve that request too.
4. Packaging continues with the signed installer, and the manifest records
   `"windows": "signpath-foundation"`.

A rejected or timed-out request fails the release. Fix the cause and rerun the
same run; SignPath evaluates up to three re-runs of a build. Never submit a file
built anywhere other than this workflow.

### Signed uninstaller

NSIS normally writes `uninstall.exe` on the user's computer during installation,
so it could never be signed. `project.nsi` has two build modes for this:

| Define | Result |
| --- | --- |
| `-DWRITE_UNINSTALLER=<path>` | Builds `uninstall-writer.exe`, which only writes the uninstaller to `<path>` and exits. CI runs it and adds the uninstaller to the `desktop-windows-app` artifact. |
| `-DSIGNED_UNINSTALLER=<path>` | Packs the uninstaller at `<path>` and writes the Uninstall registry entries itself; the script's own uninstall section is left out. |

Without either define, as in `wails build -nsis`, the installer generates its
uninstaller as before. In every mode the uninstaller first asks the app whether
the clinic setup is gone; see
[removing the desktop app](cleanup-and-uninstall.md#removing-the-desktop-app). Keep the registry entries in the `SIGNED_UNINSTALLER`
branch in step with Wails' `wails.writeUninstaller` macro when upgrading Wails.

### Before the first signed release

1. Once SignPath approves the project, create the artifact configuration and the
   signing policy there, then store the four settings above in the repository.
2. Bump `CARE_CLINIC_VERSION` past the last released version (for example `0.1.1`
   after the unsigned `0.1.0`); versions are never reused.
3. Run the release workflow and approve both signing requests.
4. Check `release-manifest.json`, then publish the draft.

## Existing macOS signing configuration

No secret names, `.env` pins, bundle identifier, or application executable name
need to change:

| Existing secret | Purpose |
| --- | --- |
| `MACOS_CERT_P12` | Base64-encoded Developer ID signing certificate and private key. Its presence enables macOS signing. |
| `MACOS_CERT_PASSWORD` | Password used to import that PKCS#12 file. |
| `MACOS_SIGN_IDENTITY` | Existing Developer ID signing identity. |
| `APPSTORE_PRIVATE_KEY` | Existing App Store Connect API private key used by `notarytool`. |
| `APPSTORE_KEY_ID` | API key ID. |
| `APPSTORE_ISSUER_ID` | API issuer ID. |

The workflow keeps the existing `SIGN_MACOS`, `SIGNING_KEYCHAIN`, `CERT_P12`,
`CERT_PASSWORD`, `SIGN_IDENTITY`, `NOTARY_KEY`, `NOTARY_KEY_ID`, and
`NOTARY_ISSUER` environment mappings. It imports into a temporary keychain,
retains the separate submit/wait/status-check notarization flow, and validates
the stapled application and DMG. Cleanup runs even after failure.

If signing is enabled, incomplete credentials or a rejected notarization fail
the release; they do not silently produce an unsigned substitute. The job allows
up to six hours for Apple's queue, subject to the hosted runner limit.
When `MACOS_CERT_P12` is absent, the manifest explicitly says `ad-hoc`.
