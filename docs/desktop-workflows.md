# Using CARE Desktop

[Documentation index](README.md)

This guide follows the desktop application from first launch through setup,
connection, installation and the server control panel. Facility and clinical
data setup happens separately in the [CARE Onboarding plugin](onboarding.md),
inside the CARE web application.

The desktop opens at 1100 by 700 and supports a minimum window of 720 by 560.
Long forms and dialogs scroll inside the window. The interface uses plain
language for failures; **Open log** provides diagnostic detail for support.

## First launch and saved roles

The Start screen offers **Start setup** and **Connect to an existing server on
the local network**. Viewing this screen saves no role and performs no cleanup.
Checking for a CARE Desktop update also does not select a role.

| Choice | When it is saved | What Back can do |
| --- | --- | --- |
| Host the clinic | `BeginServerSetup` records Server when the setup screen initializes, before guarded setup reads and writes. | Return through earlier steps; leave setup only while the backend considers the choice unused. |
| Connect to a clinic | `ConnectClient` records Client when connecting begins. Finding a clinic does not save it. | Leave before connecting. After a saved or partial connection, use Disconnect. |

An existing installation resumes its saved role rather than offering a role
switch. Deleting settings is not a supported way to change roles. See
[configuration and role guards](configuration-and-settings.md#persisted-config).

## Set up this computer as the server

Setup needs internet access for software and image downloads. The Start
screen's time estimate is not a deadline or measured installation progress.

1. **Computer checks.** CARE checks free space, required software, earlier
   installation files, and the applicable Windows setup and network requirements.
   Every applicable step stays on screen until you choose **Continue**, even when
   it already passes, finishes installing software, or passes a repeated check.
   This includes free space and checking for earlier installation files on a clean
   computer. Requirements that do not apply to this operating system are omitted.
   Required software uses green **Available** and red **Needs setup** badges, with
   a plain-language description of what each program does.
   A failed check keeps an explanation, a fix where available, and **Check again**.
   The free-space screen shows both the space needed and the amount available:
   green when sufficient, red when space needs freeing. CARE Desktop needs at least
   **30 GB** on the clinic data drive (`storage.InstallMinFree`); a separate settings
   drive also needs **1 GB**. If that separate drive is too full, its own measurements
   are shown instead. An unavailable measurement never enables Continue.
2. **Clinic address.** Choose the local clinic name. CARE validates it and checks
   for another visible server using it. Keep other clinic servers awake during
   setup: an offline or isolated computer cannot be detected. Choosing a name
   does not start advertising it.
3. **Backups.** Choose a writable backup destination. Save the private backup
   recovery file separately, then select that same file again so CARE can verify
   it. **Save a new recovery file** remains available beside file selection after
   saving. Replacing the key requires selecting and verifying the new file before
   continuing; cancelling replacement preserves the saved file and verification.
   Cancellation is not a successful save or verification. Selecting an incorrect
   or unreadable PEM clears the previous verification; the row no longer shows
   Checked and Continue stays disabled until the correct file is checked.
4. **Admin password.** Choose the Desktop password and save the sheet of six
   single-use recovery codes. The password must contain 8 through 20 Unicode
   characters, including an uppercase letter, lowercase letter and digit.
   A reminder asks the administrator to save the password in a password manager
   or write it down somewhere secure before installation starts.
   The sheet can be opened for printing through its associated application.
5. **Review.** Review the selections and use Edit or Fix to return to the relevant
   step. CARE rechecks requirements, locations and recovery files before enabling
   installation; `RunSetup` repeats the checks under the native operation lock.
   A rejected request stays on Review. A passing edited or fixed check also waits
   for **Continue** before returning directly to Review.

Once installation has actually begun, going Back cannot undo it. An incomplete
installation has its own cleanup/retry flow; it is not an unused role choice.

### Finding saved files on Windows

Windows' visible Desktop may be redirected to OneDrive or another location.
CARE uses that Windows-reported location for the default backup directory and
as the starting point for its native folder and recovery save dialogs. It does
not move an explicitly configured backup folder. After saving recovery files
or codes, **Open folder** reveals the saved file in Explorer.

The Windows backup-location screen shows free space, without estimating how
many days or years of backups will fit. Low-space errors still block setup.
Prefer an external backup drive and keep recovery materials separately;
Desktop may sync private recovery files to OneDrive. The Windows-only warning
is guidance, not cloud-folder detection. Other platforms retain their existing
save-dialog and capacity presentation.

### Keep the two recovery materials separate

| Material | Purpose |
| --- | --- |
| Backup recovery file, ending in `.pem` | Decrypt the clinic's encrypted backups. It is a private key, not a backup or password. |
| Desktop recovery-code sheet | Reset a forgotten Desktop password offline. Each code is usable once. It cannot decrypt backups or reset CARE web credentials. |

Keep secure copies outside CARE's installation, settings, logs and backup
folder, preferably away from this computer. CARE validates locations and file
contents, but cannot prove that a selected drive is physically offsite.
At installation, CARE also encrypts a local copy of the verified backup key
using the Desktop admin password. This does not replace an off-device recovery
file: losing this computer, or resetting a forgotten password with recovery
codes, can make that encrypted local copy unavailable.
Replacing a lost pre-install backup key requires an explicit new export and
verification. Keep old keys for any older backups. Replacing the recovery-code
sheet invalidates the old codes.

See [backup recovery and custody](backups-and-restore.md#3-backup-recovery-file-and-desktop-admin-recovery)
for the native checks and limitations.

## During installation

Installation shows real log-backed stages, including preparing settings,
building CARE, setting up this computer, starting the clinic and preparing the
database. The progress indicator is indeterminate: there is no measured overall
percentage or guaranteed remaining time. The old visible "earlier", "now" and
"latest" tags are not shown; current/completed stage information remains
available to assistive technology.

After fifteen minutes without a new log line, CARE identifies the **last reported
stage** and offers the log. This is a quiet-activity warning, not a failed-job
declaration or a second timer for Advanced settings.

Successful request acceptance does not mean installation succeeded. The desktop
opens Overview only after both the saved setup-success event and matching
successful job completion arrive. A failure keeps the failure screen, relevant
guidance and logs instead of entering the panel.

Initial installation needs internet to download source and image dependencies.
Recognized connection failures show **The download was interrupted**, ask the
operator to reconnect, and do not suggest updating CARE Desktop as the remedy.
Keep CARE Desktop open and choose **Try again** to retry the same unfinished
installation. The clinic address, backup folder, admin password and saved
recovery files are kept; completed source downloads and images can be reused.
Preparation that already succeeded is not repeated after a startup failure.
This is not byte-level download resumption.

This retry is available only while the original Desktop process retains the
attempt. It verifies that the saved configuration is unchanged and that the
original recovery files and backup location remain available. It never accepts
replacement settings or performs cleanup. A full app restart loses the retained
attempt; the admin password is not stored in plaintext to support restart-time
resumption.

If the native app cannot retry the original attempt, the failure screen explicitly
warns that **Try again** clears the unfinished installation and starts the backup
and password steps over. This cleanup runs only when requested, preserves
existing backups, and resets setup choices that need to be collected again.

### Permission prompts

CARE-owned setup, saved-address removal and certificate-removal confirmations
use the in-window dialog: a short explanation, **Continue**, and **Cancel**.
Cancel initially has focus; Escape declines. The dialog does not collect an
operating-system password.

The next prompt may come from macOS, Windows or Linux itself. Enter the
computer's administrator password or approve the system request there. macOS can
require a separate certificate security approval. These native prompts are not
replaced or bypassed by the desktop dialog.

The effect of declining depends on the operation. Optional setup for opening
CARE on the server computer can be skipped without stopping the clinic.
Incomplete removal is reported and remains retryable; it is not silently marked
clean. [Native integration details](native-integrations.md#administrator-approval-and-local-hosts-setup)
explain which changes are attempted and verified.

## Connect another computer

Use the clinic address shown on the server and a trusted clinic network.

1. Choose **Connect to an existing server on the local network**, enter the
   clinic name, and choose **Find clinic**. This discovery step installs nothing.
2. Review the found clinic, then choose **Connect**. If an old address override
   prevents discovery, **Connect and fix** is the explicit repair path.
3. Approve the computer's permission requests when needed. Connecting removes
   conflicting CARE-owned trust and address settings, installs the clinic's
   certificate and verifies the secure connection before opening CARE.
4. Use **Open CARE** from the connected screen. Its periodic availability check
   is read-only. Offline or verification failures do not erase the saved
   connection; use the displayed retry or repair guidance.

Connect is the consent step; it does not add another CARE confirmation in front
of the operating system's approval. A partially completed connection retains
enough information to retry or **Disconnect**. Disconnect removes this client's
owned access, not the server's records. Certificates not installed by this
client are preserved.

Initial certificate bootstrap is trust on first use over local HTTP. The
automatic TLS checks do not rule out impersonation on an untrusted network.
See [client trust](native-integrations.md#native-client-setup-and-trust-on-first-use)
and [earlier-installation recovery](client-recovery.md).

## Server control panel

| Tab | What to use it for |
| --- | --- |
| Overview | Clinic health, Start/Stop/Restart, opening CARE, startup at login, phone/tablet connection, backup summary and actionable problems. |
| Backups | Actual backup policy and recent backup state, Back up now, backup destination management and restoring a selected file. |
| Storage | Drive measurements and explicit cleanup of supported disposable Docker resources, not clinic records. |
| Updates | Separate CARE backend/frontend updates and CARE Desktop application updates. |
| Plugins | Add catalog or custom plugins and Save and apply from a healthy running clinic. No Desktop password is required. Entries stay **Not applied** until successful; a failed batch is discarded when leaving the tab. Failed loading rolls back to the previous configuration, and unfinished recovery exposes **Recover clinic** in Overview. |
| Advanced | Ten everyday clinic settings, support-only extra overrides, Desktop password/recovery management, diagnostic log, rebuild and uninstall. |

**Connect phone or tablet** displays a real QR code for
`http://<clinic>.local/setup`. Phones and tablets still need to follow their
platform's certificate instructions; displaying or scanning the code is not
proof that trust was installed.

When two or fewer admin recovery codes remain, a persistent banner appears at
the top of every control-panel tab. It shows the unused count, turns red at zero,
and returns after restarting the app. **Save new recovery codes** asks for the
current Desktop admin password before opening the save dialog for six new codes.
Saving a new set invalidates every previous code. Cancelling or failing to save
does not clear the reminder. Successful replacement in either the banner or
Advanced clears it; using a recovery code updates the count immediately.

Storage highlights the **currently used** amount under **CARE Desktop storage**,
with Rancher Desktop's total capacity shown separately rather than as usage.
This is usage inside the shared Rancher Desktop disk, including supporting
software and potentially other apps; it is not an exact CARE-only measurement
or the disk image's physical size on the host computer.

The Overview backup summary shows useful status, size and encryption information
without a `saved to /...` filesystem path. The actual destination remains
available where it can be managed in Backups.

### Backups and restore

Scheduled backups run at a 24-hour interval while their service is running, not
at a fixed nightly clock time. The displayed retention comes from the installed
settings; zero means keep backups indefinitely. Start CARE before taking a
manual backup.

**Re-download backup key** asks for the current Desktop admin password and saves
another copy of the original recovery file. After installation or explicit
enrollment, CARE can unlock its encrypted local copy without asking you to find
the original file. For an older installation without a usable encrypted copy,
use the tracked recovery file or **Select another saved copy** to enroll a
compatible PEM file. The **Enable password-only downloads** notice explains
enrollment; **Choose where to save** exports the PEM and saves its encrypted
local copy. CARE verifies the configured and installed public certificates and
never rotates the installed backup key. A corrupt local copy requires explicitly
selecting a surviving PEM rather than silently falling back to a file.
Cancelling the save leaves enrollment unchanged. If export succeeds but local
enrollment cannot be saved, CARE reports that distinction: keep the exported
PEM and retry enrollment.

Changing the Desktop admin password re-encrypts an enrolled, usable local key
with the new password; a failure leaves both unchanged. A forgotten-password
reset using recovery codes cannot unlock the
previously encrypted key; CARE preserves that encrypted data and requires an
explicit recovery-file enrollment to enable export again. Keep an off-device
PEM file even when re-download is available. If no usable encrypted copy or
external recovery file remains, CARE cannot reconstruct the private key.

Use **Restore from a backup file** for a file in the current backup folder or
one copied from elsewhere. The dialog identifies the selected database dump and
any matching uploaded-files archive. Select the matching recovery file when
encrypted, enter the Desktop password and explicitly acknowledge replacement.
A database-only restore leaves uploaded files as they are.

Cancel is available during file selection and read-only preflight. It clears
the password and replacement acknowledgement while preserving the previous file
selection. Once a restore request is submitted, the dialog cannot cancel the
native job. Do not close the app to simulate cancellation. If a restore remains
unfinished, use Start from Overview to run its recovery path before starting
another restore.

### Advanced's 15-minute unlock

Clinic settings are grouped into **Clinic details**, **Patients and visits**,
**Billing**, **Backups** and **Staff access**. **Save changes** applies the
selected preferences and may briefly interrupt staff using CARE. Less-used
options keep their defaults or previously saved values. Only open
**Extra settings (for support)** when your CARE support person asks you to;
its values override the defaults. Email, SMS and MFA settings remain excluded.

Click a category to expand its settings inline; click it again to collapse it,
or choose another category to switch. The Advanced header, other categories,
password/recovery tools, log and removal controls stay on the same page.
Collapsing or switching with unsaved edits asks whether to keep editing or
discard them; navigation never saves settings. Each expanded category contains
its own save/discard controls and apply status.

Advanced locks **15 minutes after a successful unlock**, even while the user is
active. It is not an inactivity timeout. Leaving the tab or using **Lock** locks
it sooner. Locking clears the retained password and unsaved or sensitive form
state; it does not undo already saved settings or cancel an accepted native job.

This is a frontend timer, not a backend session or token. Protected Go methods
verify the supplied Desktop password for each operation. The Desktop password
and recovery codes do not change the CARE web login after initial setup.

### Updates and running work

CARE updates stage backend/frontend builds from the configured branch. A failed
check remains a retryable failure, not "Up to date". Installing a staged CARE
update can interrupt service.

A CARE Desktop update downloads a verified application installer or replaces
the app bundle. Conflicting actions stay locked while it runs:

| Desktop update state | How to proceed |
| --- | --- |
| Checking, available, or failed check | Continue ordinary work, retry the check, or explicitly start the update. |
| Downloading, verifying or installing | Keep CARE Desktop open and wait. |
| External installer opened | Follow the installer. After the native handoff completes, **OK** on start/client/setup or **Done** in Updates acknowledges the handoff and releases the interface lock. |
| Reopening | Wait for the application to reopen. There is no OK/Done action that releases this state. |
| Failed download or installation | Read the short failure message and retry when ready. |

A handoff acknowledgement does not prove the external installer finished and
does not change the running version to "Up to date". File pickers and restore
preflight started before an update cannot resume a stale change after
acknowledgement; repeat the interrupted action explicitly.

## Closing and removing CARE

Closing during a native job asks whether to quit or **Keep waiting**; it never
means the job succeeded. Already launched installers and processes are not
automatically stopped. Closing an idle desktop and stopping the clinic are
separate choices; macOS can also hide the window without exiting the process.

Server removal belongs in Advanced and requires the Desktop password and
explicit deletion choices. Completion is acknowledged only after both native
cleanup success and the corresponding job completion. Partial failures keep the
removal flow available. Removing the desktop executable is a separate optional
step on supported installations.

A central, non-dismissible progress message asks you to wait and keep the
window open throughout removal, including an optional desktop-app handoff.
Native permission confirmations remain usable above that message. Progress
does not end on the first success event; it waits for both `uninstalled` and
successful `care-done`. A rejected start restores the deletion confirmation
with a retry error; a failed running job clears progress and shows the failure.

See [cleanup and uninstall](cleanup-and-uninstall.md) before deleting clinic
data, images or backups. Use a disposable environment for installation, restore,
trust/network changes and removal experiments; the real desktop is not a demo.
