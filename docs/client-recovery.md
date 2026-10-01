# Cannot open CARE from a computer that previously hosted a clinic

Use this guide when a Windows, macOS, or Linux computer **previously ran CARE
Desktop as a server**, but now needs to connect to a different CARE server.
Other devices may open the clinic normally while this computer times out.

This is a recovery procedure for an earlier installation, not a setup
requirement for every client. The saved Server/Client role is not an ordinary
switch: do not delete configuration or clinic data merely to change it.

For an ordinary CARE Desktop client leaving a clinic, use **Disconnect**, not the server-uninstall procedures below. It removes only that
client's saved connection and certificate it installed, preserving all server data.
Successful uninstall clears the role so Server or Client can be selected again.
If you also want to remove the desktop executable, uninstall it through the
operating system afterwards. Trusted certificates that did not come from CARE
are intentionally preserved and may still allow browser access.
See [client removal](native-integrations.md#removing-client-access).

## Why this happens

During local server setup, CARE can add a line such as:

```text
127.0.0.1 care.local # care-desktop
```

That makes this computer resolve `care.local` to **itself**, rather than discover
the real server on the clinic network. The entry can remain even when Docker is
not running or CARE Desktop has been deleted. Reinstalling a certificate or
rebuilding the frontend will not fix that address override.

## The fix: connect as a client

CARE Desktop's client setup removes this override automatically. On the affected
computer, choose **Use as client**, enter the clinic address shown on the current
server, and click **Connect**.

**Connect** repairs this computer's hosts file, on its own and without asking
a second time — pressing Connect is the decision. Your computer asks for
permission the way it always does: a password, a fingerprint or a PIN. On a Mac
it can ask twice, once for the address override and once for the certificate,
because macOS handles those two things separately. It removes the address you
entered, whoever added it, *and* every line CARE itself added, whatever name
that line maps — so an entry left over from a clinic this computer used to host
under a different name goes too. It saves the old file as `hosts.care-backup`
and clears the DNS cache. In the same approval it removes every other
`CARE Desktop Local CA` certificate this computer trusts and installs the
current clinic's, so you are asked for administrator approval once. If there is
nothing to repair, you won't see a prompt at all. The check runs again every
time you click **Connect** or **Open CARE**, so an entry that comes back later
is also removed. See [the hosts check](native-integrations.md#hosts-entries-and-their-ownership-marker)
and [client trust](native-integrations.md#native-client-setup-and-trust-on-first-use).

**Find clinic** is the step before it, and it changes nothing at all: it only
looks for the clinic and checks that it really is a CARE clinic. Use it to
confirm the address before anything is repaired. On a computer whose hosts file
still points the address at itself, it asks the network directly rather than the
address override, so it can normally still find the real clinic. If the network
does not answer, it says **"this computer is sending the clinic address to
itself"** — click **Connect**, which fixes exactly that.

This does not remove the old clinic's Docker data, backups, or the application.
Starting the old clinic again on this computer can add its entry back. Uninstall
the old clinic if it is no longer needed (below).

If the prompt is declined, the app shows **"Your computer needs a quick fix
first"**; click **Connect** again and approve it. If connecting still fails after
that, the hosts file is not the problem. Check Wi-Fi, mDNS, and the server
instead (see the end of this guide).

If the computer still has an unfinished clinic setup of its own — installation
files, or a half-finished wizard — connecting is refused with **"this computer
has an unfinished clinic setup; remove it in Setup before connecting"**. Remove
it from the setup screen first; that is the same cleanup described below.

## A different cause of the same message

"We couldn't find the clinic" can also appear on a healthy network with no hosts
entry at all. Before CARE Desktop 0.1.5, client connections resolved the clinic
address over both IPv4 and IPv6. A `.local` name has no IPv6 record, and macOS
waits five seconds before abandoning that half of the lookup — slightly longer
than the connection's own five-second limit. The attempt was therefore abandoned
moments before the usable IPv4 address arrived, and the log recorded:

```text
could not reach the clinic; check its address, network and main computer:
Get "http://care.local/root.crt?ok=1": dial tcp: lookup care.local: i/o timeout
```

Current versions look up IPv4 only and connect in well under a second. The
distinguishing symptom is that the clinic is reachable by other means from the
same computer while CARE Desktop reports it as missing: opening
`http://<clinic>.local/root.crt` in a browser works, and the failure takes
almost exactly five seconds every time. If you see this, update CARE Desktop on
the **client** computer; the server needs no change. See [the IPv4 rule for
clients](native-integrations.md#clients-resolve-the-clinic-over-ipv4-only).

## Safety net: standalone cleanup of an unwanted earlier installation

If the earlier installation is no longer needed and the app cannot open, is
already gone, or its removal did not finish, the standalone scripts provide a
fallback to the app's cleanup flows:

- [Windows cleanup script](../uninstall-windows.ps1)
- [macOS cleanup script](../uninstall-macos.sh)

**These are full-uninstall scripts, not a fix for the address override. They delete the old
clinic's live database/file-storage volumes, containers, installed files, and
settings. They also remove CARE certificate trust and other local integration.**
Keeping the backup folder is not the same as keeping the live clinic data.

Before proceeding, confirm this is not the active clinic server and preserve a
current, verified backup and its recovery material outside the installation.
Download the appropriate script as a file and review it; do not pipe a download
directly into a shell. Run it as the normal user who installed CARE, from the
folder containing the downloaded script. The Docker engine (Rancher Desktop on
macOS and Windows) must be running if
Docker resources are to be inspected and removed.

### Windows

Open a normal PowerShell window, **not** an administrator window. First inspect
what would be removed:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\uninstall-windows.ps1 -DryRun
```

Only after reviewing that output and deciding to delete the old installation:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\uninstall-windows.ps1
```

The script asks for confirmation and elevates the system-cleanup steps when
needed. Read any reported failures; an administrator prompt alone is not proof
that cleanup succeeded.

### macOS

Run from Terminal as your normal user, **without prefixing the script with
`sudo`**. First inspect:

```sh
bash ./uninstall-macos.sh --dry-run
```

Only after reviewing that output and deciding to delete the old installation:

```sh
bash ./uninstall-macos.sh
```

The script requests administrator approval for the steps that need it. Read any
reported failures before assuming the computer is clean.

### What to keep and what to do afterwards

The scripts retain backups by default and attempt to preserve their recovery
key. **Do not add `-RemoveBackups` / `--remove-backups` for client recovery.** Do
not use `-Yes` / `--yes` to bypass the confirmation. There is no standalone
Linux cleanup script in this repository; use the app's explicit uninstall flow,
or just connect as a client (above) if only the address override is the problem.

Because full cleanup removes CARE certificates, use CARE Desktop's native
client setup to trust the **current server's** certificate. Enter the `.local`
address shown on that server, approve the operating-system prompt if requested,
and let CARE verify HTTPS automatically. Use a trusted clinic network: the
initial HTTP certificate download is trust on first use, not independent proof
of the server's identity. Do not bypass browser certificate warnings.

If the hostname still cannot be found or the connection still times out, do not
keep rerunning the cleanup script. There may be a separate Wi-Fi, mDNS, routing,
or server problem. See [client/network limits](native-integrations.md#client-independence-and-network-limits).
