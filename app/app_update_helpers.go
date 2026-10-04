package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
)

func macUpdateHelper(target, staged, work string, pid int, elevated bool, verification ...string) string {
	q := elevate.ShQuote
	swap := swapScript(target, staged, verification...)
	install := "/bin/sh -c " + q(swap)
	if elevated {
		install = "/usr/bin/osascript -e " + q("do shell script "+elevate.OSAQuote(swap)+" with administrator privileges")
	}
	message := "CARE Clinic could not finish updating or reopening. Your clinic data has not been changed. " +
		"Open CARE Clinic from its installed folder. If it is missing, keep the .care-update- recovery folder beside it and contact support. Update log: " +
		filepath.Join(work, "helper.log")
	return strings.Join([]string{
		"#!/bin/sh",
		"umask 077",
		"exec >>" + q(filepath.Join(work, "helper.log")) + " 2>&1",
		"failed() { /usr/bin/osascript -e " + q("display alert "+elevate.OSAQuote("CARE Clinic update didn't finish")+
			" message "+elevate.OSAQuote(message)+" as critical") + "; /usr/bin/open " + q(target) + "; exit 1; }",
		"printf ready >" + q(filepath.Join(work, "ready")),
		"n=0",
		"while [ ! -f " + q(filepath.Join(work, "continue")) + " ]; do n=$((n+1)); [ \"$n\" -lt 60 ] || exit 1; /bin/sleep 0.5; done",
		"n=0",
		fmt.Sprintf("while /bin/kill -0 %d 2>/dev/null; do n=$((n+1)); [ \"$n\" -lt 240 ] || { echo 'CARE Clinic did not exit; nothing was replaced'; failed; }; /bin/sleep 0.5; done", pid),
		"/usr/bin/codesign --verify --deep --strict " + q(staged) + " || failed",
		install + " || failed",
		"/usr/bin/open " + q(target) + " || failed",
		"/bin/rm -rf " + q(work),
	}, "\n") + "\n"
}

func windowsUpdateHelper(exe, installer, work, version, digest string, pid int) string {
	q := elevate.PSQuote
	// Start-Process joins ArgumentList into one command line. NSIS requires /D
	// last and unquoted, including paths with spaces; it consumes the remainder.
	args := "/S /CAREUPDATE=1 /D=" + filepath.Dir(exe)
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$exe = " + q(exe),
		"$installer = " + q(installer),
		"$work = " + q(work),
		"$log = Join-Path $work 'helper.log'",
		"$ready = $false",
		"try {",
		"  Start-Transcript -Path $log -Force | Out-Null",
		fmt.Sprintf("  $parent = Get-Process -Id %d -ErrorAction Stop", pid),
		"  $null = $parent.Handle",
		"  $key = 'HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\Open Healthcare Network FoundationCARE Clinic'",
		"  $registered = Get-ItemProperty -LiteralPath $key",
		"  $uninstaller = Join-Path (Split-Path -LiteralPath $exe) 'uninstall.exe'",
		"  if ($registered.UninstallString -ne ('\"' + $uninstaller + '\"')) { throw 'update location unavailable: this copy does not match the registered Windows installation' }",
		"  $installedSignature = Get-AuthenticodeSignature -LiteralPath $exe",
		"  function Confirm-Download {",
		"    if ((Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash -ne " + q(digest) + ") { throw 'The verified installer changed on disk' }",
		"    $signature = Get-AuthenticodeSignature -LiteralPath $installer",
		"    if ($installedSignature.Status -eq 'Valid' -and ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -ne $installedSignature.SignerCertificate.Subject)) { throw 'The update signature does not match the installed publisher' }",
		"  }",
		"  Confirm-Download",
		"  [IO.File]::WriteAllText((Join-Path $work 'ready'), 'ready')",
		"  $ready = $true",
		"  $deadline = [DateTime]::UtcNow.AddSeconds(30)",
		"  while (!(Test-Path -LiteralPath (Join-Path $work 'continue'))) {",
		"    if ([DateTime]::UtcNow -gt $deadline) { throw 'The update handoff was not authorized' }; Start-Sleep -Milliseconds 100",
		"  }",
		"  if (!$parent.WaitForExit(120000)) { throw 'CARE Clinic did not exit; nothing was replaced' }",
		"  Confirm-Download",
		"  $setup = Start-Process -FilePath $installer -Verb RunAs -ArgumentList " + q(args) + " -PassThru -Wait",
		"  if ($setup.ExitCode -ne 0) { throw ('Installer failed (exit ' + $setup.ExitCode + '). The previous executable was retained or restored; keep any .care-update-previous.exe recovery file.') }",
		"  $actual = (Get-Item -LiteralPath $exe).VersionInfo.ProductVersion",
		"  if ($actual -ne " + q(version) + " -and $actual -ne " + q(version+".0") + ") { throw ('Installer did not install the requested version; found ' + $actual) }",
		"  Start-Process -FilePath $exe -WorkingDirectory (Split-Path -LiteralPath $exe)",
		"  Stop-Transcript | Out-Null",
		"  Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue",
		"} catch {",
		"  $detail = $_.Exception.Message",
		"  try { Stop-Transcript | Out-Null } catch { }",
		"  $detail | Out-File -LiteralPath $log -Append",
		"  if ($ready) {",
		"    Add-Type -AssemblyName PresentationFramework",
		"    [System.Windows.MessageBox]::Show(('CARE Clinic could not finish updating or reopening. Clinic data has not been changed.' + [Environment]::NewLine + $detail + [Environment]::NewLine + 'Update log: ' + $log), 'CARE Clinic update did not finish') | Out-Null",
		"    if ($parent.HasExited -and (Test-Path -LiteralPath $exe)) { Start-Process -FilePath $exe -WorkingDirectory (Split-Path -LiteralPath $exe) }",
		"  }",
		"  exit 1",
		"}",
	}, "\r\n") + "\r\n"
}
