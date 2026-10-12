"""Build CARE's portable QEMU bundle from the pinned Windows installer."""

import argparse
import hashlib
import json
import shutil
import subprocess
import tempfile
import urllib.request
import zipfile
from pathlib import Path

PIN = Path(__file__).with_name("qemu-windows.json")


def digest(path, algorithm):
    h = hashlib.new(algorithm)
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def bundle_files(root, version):
    if (root / "VERSION").read_text().strip() != version:
        raise ValueError("Installer VERSION does not match the QEMU pin")
    for name in (
        "qemu-system-x86_64.exe",
        "libslirp-0.dll",
        "share/bios-256k.bin",
        "COPYING",
        "COPYING.LIB",
        "README.rst",
    ):
        if not (root / name).is_file():
            raise ValueError(f"QEMU installer is missing {name}")
    files = [root / name for name in (
        "qemu-system-x86_64.exe", "VERSION", "COPYING", "COPYING.LIB", "README.rst"
    )]
    files.extend(root.glob("*.dll"))
    for name in ("share", "lib"):
        if (root / name).is_dir():
            files.extend(p for p in (root / name).rglob("*") if p.is_file())
    return sorted(set(files), key=lambda p: p.relative_to(root).as_posix())


def package(root, output, pin):
    files = bundle_files(root, pin["version"])
    provenance = {
        "version": pin["version"],
        "installer_url": pin["url"],
        "installer_sha512": pin["sha512"],
        "files_sha256": {
            p.relative_to(root).as_posix(): digest(p, "sha256") for p in files
        },
    }
    # Stored entries avoid zlib/version differences; fixed metadata and ordering
    # make identical installer contents produce identical bytes on every host.
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as archive:
        for p in files:
            info = zipfile.ZipInfo(p.relative_to(root).as_posix(), (1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            with p.open("rb") as source, archive.open(info, "w") as target:
                shutil.copyfileobj(source, target)
        info = zipfile.ZipInfo("care-qemu-provenance.json", (1980, 1, 1, 0, 0, 0))
        info.create_system = 3
        info.external_attr = 0o100644 << 16
        archive.writestr(info, json.dumps(provenance, sort_keys=True, indent=2) + "\n")
    return digest(output, "sha256")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--installer", type=Path, help="Use a local, checksum-verified installer")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    pin = json.loads(PIN.read_text())
    extractor = shutil.which("7z") or shutil.which("7zz")
    if extractor is None:
        raise RuntimeError("7-Zip is required to extract the NSIS installer")
    args.output.mkdir(parents=True, exist_ok=True)
    name = f"qemu-{pin['version']}-windows-x86_64.zip"
    output = args.output / name
    if output.exists():
        raise FileExistsError(f"Refusing to overwrite {output}")
    with tempfile.TemporaryDirectory() as temp:
        temp = Path(temp)
        installer = args.installer
        if installer is None:
            installer = temp / "installer.exe"
            with urllib.request.urlopen(pin["url"], timeout=120) as response, installer.open("wb") as target:
                shutil.copyfileobj(response, target)
        actual = digest(installer, "sha512")
        if actual != pin["sha512"]:
            raise ValueError(f"Installer SHA-512 mismatch: expected {pin['sha512']}, got {actual}")
        root = temp / "extracted"
        subprocess.run([extractor, "x", str(installer.resolve()), f"-o{root}", "-y"],
                       check=True, stdout=subprocess.DEVNULL)
        staged = temp / name
        checksum = package(root, staged, pin)
        shutil.move(staged, output)
    print(f"QEMU_WINDOWS_VERSION={pin['version']}")
    print(f"QEMU_WINDOWS_SHA256={checksum}")
    print("Packaging is complete; Windows validation is required before publication.")


if __name__ == "__main__":
    main()
