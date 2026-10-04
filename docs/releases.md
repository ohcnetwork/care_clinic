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

## Prepare a version

1. Create a PR updating `deployments/.env`.
2. Set `CARE_CLINIC_VERSION` to a new numeric version, for example `0.1.1`.
   Use a new version even if only the backend, frontend, images, or deployment
   settings changed.
3. Update the dependency pins needed by that release.
4. Review the PR, let CI pass, and merge it into `main`. The merge starts the
   release; see [Build the release](#build-the-release).

| Configuration | What to maintain |
| --- | --- |
| `CARE_CLINIC_VERSION` | One `X.Y.Z` value. `-dev` builds are not accepted by the release workflow. |
| `CARE_BE_REPO`, `CARE_FE_REPO` | The intended CARE source repositories. |
| `CARE_BE_REF`, `CARE_FE_REF` | The branch of verified CARE commits that installed clinics follow, normally `develop`. A full 40-character commit SHA pins that service instead. Tags are not supported. |
| `POSTGRES_IMAGE`, `REDIS_IMAGE`, `MINIO_IMAGE`, `CADDY_IMAGE` | Deliberately chosen image versions; use immutable digests when available. |
| `CORAZA_VERSION` | The WAF module version used to build the proxy. |
| `BACKUP_IMAGE`, `CADDY_WAF_IMAGE`, `BACKEND_IMAGE`, `FRONTEND_IMAGE` | Local output image names. These are not upstream version selectors; normally leave them alone. |
| `RANCHER_*`, `GIT_WINDOWS_*`, `DOCKER_LINUX_*`, `COMPOSE_LINUX_*` | The exact Rancher Desktop, Git for Windows, and Linux Docker Engine/Compose downloads the app installs. See [below](#bump-a-pinned-prerequisite). |

Changing these branches changes what every installed clinic follows from its
next update check onward, not only what this release ships. Only point them at
a branch whose commits are verified.

Do not add passwords, signing credentials, or clinic-specific data to this file:
the exact file is embedded in the app and attached to the release.

### Bump a pinned prerequisite

Only bump to a stable upstream release, and test the new installer on a clean
machine for each platform before releasing it.

1. Set the version without a leading `v`: `RANCHER_VERSION` (for example
   `1.24.0`), `GIT_WINDOWS_VERSION` (the Git for Windows tag, for example
   `2.55.0.windows.5`), `DOCKER_LINUX_VERSION` (a `docker-<version>.tgz` listed at
   `download.docker.com/linux/static/stable/x86_64/`, for example `29.8.1`), or
   `COMPOSE_LINUX_VERSION` (the `docker/compose` release, for example `5.5.1`).
2. Compute the SHA-256 of each file from its official release page:

   ```sh
   v=1.24.0; r=https://github.com/rancher-sandbox/rancher-desktop/releases/download/v$v
   for f in Rancher.Desktop-$v.aarch64.dmg Rancher.Desktop-$v.x86_64.dmg Rancher.Desktop.Setup.$v.msi; do
     curl -fsSL "$r/$f" | shasum -a 256
   done
   curl -fsSL https://github.com/git-for-windows/git/releases/download/v2.55.0.windows.5/Git-2.55.0.5-64-bit.exe | shasum -a 256
   for a in x86_64 aarch64; do
     curl -fsSL https://download.docker.com/linux/static/stable/$a/docker-29.8.1.tgz | shasum -a 256
     curl -fsSL https://github.com/docker/compose/releases/download/v5.5.1/docker-compose-linux-$a | shasum -a 256
   done
   ```

3. Put each hash in its `*_SHA256` key. Rancher also publishes a
   `<file>.sha512sum` beside each installer, and Compose a
   `docker-compose-linux-<arch>.sha256`; checking against them confirms the file
   you hashed is the one upstream released.

The release workflow downloads each pinned installer again and fails if any hash
differs, so a wrong or missing value cannot reach a draft.

`deployments/.env` is the version source of truth. Do not maintain
`app/wails.json`'s `info.productVersion` by hand. The staging script synchronizes
it before native builds; a stale checked-in metadata version is not a separate
release input.

## Build the release

Merging the version PR into `main` runs **Release CARE Clinic**. Its first
job, **Decide whether to release**, compares `CARE_CLINIC_VERSION` with the
commit before the push: when it changed, the release continues; when only other
`.env` values changed (an image or prerequisite pin, a CARE branch), every
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
| Validate | Reject malformed/duplicate version values, FE/BE refs that are not a branch name or commit SHA, and a version that already has a draft or published release. |
| Verify prerequisites | In parallel with validation, download every pinned Rancher Desktop, Git for Windows, Docker Engine, and Compose file from upstream and compare its SHA-256 with `deployments/.env`. A missing file or a different hash fails the release before anything is built. |
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

The process does not deploy to clinics, bundle Docker/prerequisite installers,
or prebuild the upstream CARE container images. Initial clinic setup still
needs the dependencies and source downloads described in the installation
guides. Image tags and downstream dependency downloads are not guaranteed
immutable.

A published release does reach installed clinics on its own, by two separate
routes, both described in
[clinic lifecycle](clinic-lifecycle.md) and
[configuration and settings](configuration-and-settings.md):

- **CARE backend and frontend** follow the branch named in the manifest. An
  installed clinic checks it in the background, builds the newer commit, and
  applies it when the operator accepts or at the next start. This needs no
  desktop release at all, which is the point: a verified fix merged to the
  branch reaches clinics that nobody will manually update.
- **CARE Clinic** is offered from this release page under Advanced ->
  Updates, and is always operator-initiated.

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
