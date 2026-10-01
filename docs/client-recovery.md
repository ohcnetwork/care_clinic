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
On a supported installed copy, Disconnect can also offer removal of the desktop
application after access cleanup succeeds; otherwise use the operating system
afterwards. Trusted certificates that did not come from CARE
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
computer, choose **Connect to an existing server on the local network**, enter
the clinic name shown on the server, and choose **Find clinic**. Confirm the
found clinic with **Connect**. Discovery does not save a role or change trust;
the connection attempt is where the client role and cleanup state are saved.

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
does not answer and the local override is detected, the interface says
**This computer is pointing the clinic address at itself**. Choose
**Connect and fix** to explicitly repair it and retry discovery.

This does not remove the old clinic's Docker data, backups, or the application.
Starting the old clinic again on this computer can add its entry back. Uninstall
the old clinic if it is no longer needed (below).

If approval is declined, the app reports the specific failed permission or
repair. Hosts cleanup can show **Your computer needs a quick fix first**;
certificate approval can show **Permission was not given** or
**Your computer didn't allow the connection**. Retry and approve the OS prompt,
or ask the computer's administrator for help. A saved partial connection keeps
Disconnect available. If another error remains, check Wi-Fi, mDNS and the server
rather than assuming the repair succeeded or was the only problem.

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

## Removing an unwanted earlier installation

This checkout does not ship standalone uninstall scripts. Use the desktop's
guarded [cleanup and uninstall flows](cleanup-and-uninstall.md), not an old
terminal recipe or a downloaded script whose behavior has not been reviewed.

**Server uninstall and residue cleanup delete live clinic data.** They are not
the first remedy for a client address override. Before proceeding, confirm that
this is not the active server and preserve a verified backup and its matching
recovery file outside the installation.

| Current state | Supported path |
| --- | --- |
| Installed server | Advanced, unlock with the Desktop password, then Remove CARE from this computer. Review the backup/image choices. |
| Unfinished setup | The setup or failed-install screen's explicit cleanup path. Existing backups are kept, but partial live resources are removed. |
| Saved client connection | Disconnect. It does not remove server records. |

If the executable was deleted, restore a compatible official CARE Desktop copy
under the same OS account and use the appropriate flow. Launching the real app
can start an installed clinic or advertise its name, so treat this as maintenance
work, not a read-only inspection. If the app cannot start, preserve its settings,
logs and recovery materials and ask support before manually removing resources.
Do not delete `config.json` merely to make the role choice reappear.

### After cleanup

Because full cleanup removes CARE certificates, use CARE Desktop's native
client setup to trust the **current server's** certificate. Enter the `.local`
address shown on that server, approve the operating-system prompt if requested,
and let CARE verify HTTPS automatically. Use a trusted clinic network: the
initial HTTP certificate download is trust on first use, not independent proof
of the server's identity. Do not bypass browser certificate warnings.

If the hostname still cannot be found or the connection still times out, do not
keep repeating destructive cleanup. There may be a separate Wi-Fi, mDNS, routing,
or server problem. See [client/network limits](native-integrations.md#client-independence-and-network-limits).
