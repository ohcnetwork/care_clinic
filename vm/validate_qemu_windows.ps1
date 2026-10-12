param(
    [Parameter(Mandatory = $true)][string]$Archive
)
$ErrorActionPreference = "Stop"
$pin = Get-Content "$PSScriptRoot/qemu-windows.json" -Raw | ConvertFrom-Json
$temp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
try {
    Expand-Archive -LiteralPath $Archive -DestinationPath $temp
    $manifest = Get-Content "$temp/care-qemu-provenance.json" -Raw | ConvertFrom-Json
    if ($manifest.version -ne $pin.version -or $manifest.installer_sha512 -ne $pin.sha512 -or $manifest.installer_url -ne $pin.url) {
        throw "Bundle provenance does not match the pinned installer"
    }
    foreach ($file in $manifest.files_sha256.PSObject.Properties) {
        $actual = (Get-FileHash (Join-Path $temp $file.Name) -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $file.Value) { throw "Bundle file hash mismatch: $($file.Name)" }
    }
    $exe = Join-Path $temp "qemu-system-x86_64.exe"
    $version = @(& $exe --version)
    if ($LASTEXITCODE -ne 0 -or $version[0] -notmatch "^QEMU emulator version $([regex]::Escape($pin.version))(\s|$)") {
        throw "Repackaged QEMU does not run at the pinned version"
    }
    $accelerators = & $exe -accel help
    if ($LASTEXITCODE -ne 0 -or $accelerators -notcontains "whpx") {
        throw "QEMU was built without WHPX"
    }
    # Hosted runners cannot exercise WHPX. TCG still checks q35 firmware,
    # virtio devices and user networking without starting a clinic or guest.
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $exe
    $start.UseShellExecute = $false
    $start.RedirectStandardInput = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($arg in @(
        "-machine", "q35", "-accel", "tcg", "-cpu", "max", "-m", "128",
        "-S", "-display", "none", "-serial", "none", "-monitor", "stdio",
        "-L", (Join-Path $temp "share"),
        "-blockdev", "driver=null-co,node-name=care-data,size=1048576",
        "-device", "virtio-blk-pci,drive=care-data",
        "-device", "virtio-net-pci,netdev=net0",
        "-netdev", "user,id=net0,hostfwd=tcp:127.0.0.1:0-:80",
        "-device", "virtio-rng-pci"
    )) { $start.ArgumentList.Add($arg) }
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        $process.StandardInput.WriteLine("quit")
        $process.StandardInput.Close()
        if (-not $process.WaitForExit(30000)) {
            $process.Kill($true)
            $process.WaitForExit()
            throw "QEMU runtime validation timed out"
        }
        if ($process.ExitCode -ne 0) { throw "QEMU runtime validation failed: $($stderr.Result)" }
        Write-Output $stdout.Result
    } finally {
        if (-not $process.HasExited) { $process.Kill($true); $process.WaitForExit() }
        $process.Dispose()
    }
    Write-Output "Validated pinned QEMU, WHPX build support, q35, virtio and user networking."
    Write-Output "Actual WHPX guest boot still requires a disposable Windows machine."
} finally {
    if (Test-Path $temp) { Remove-Item -LiteralPath $temp -Recurse -Force }
}
