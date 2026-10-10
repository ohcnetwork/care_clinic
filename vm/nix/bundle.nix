{ pkgs, guest, version }:
let
  cfg = guest.config;
  arch = if pkgs.stdenv.hostPlatform.isAarch64 then "arm64" else "amd64";
  kernel = if pkgs.stdenv.hostPlatform.isAarch64 then "${cfg.microvm.kernel}/Image" else "${cfg.microvm.kernel}/bzImage";
in
pkgs.runCommand "care-engine-${version}-${arch}" { nativeBuildInputs = [ pkgs.e2fsprogs pkgs.python3 pkgs.gnutar pkgs.gzip ]; } ''
  mkdir -p bundle
  cp ${kernel} bundle/kernel
  cp ${cfg.microvm.initrdPath} bundle/initrd
  cp ${cfg.microvm.storeDisk} bundle/system.img
  # Small formatted seed only: the host grows its private copy sparsely and the
  # guest grows ext4 on boot (fileSystems."/var/lib".autoResize).
  truncate -s 64M bundle/data.img
  mkfs.ext4 -q -F -L care-data bundle/data.img
  export BUNDLE=$PWD/bundle
  python - <<'PY'
  import os, json, hashlib, pathlib
  root = pathlib.Path(os.environ["BUNDLE"])
  files = {}
  for name in ["kernel", "initrd", "system.img", "data.img"]:
      with (root / name).open("rb") as f:
          files[name] = hashlib.file_digest(f, "sha256").hexdigest()
  (root / "manifest.json").write_text(json.dumps({
      "version": "${version}",
      "arch": "${arch}",
      "kernel": "kernel",
      "initrd": "initrd",
      "system": "${builtins.unsafeDiscardStringContext cfg.system.build.toplevel}",
      "cmdline": "console=hvc0 console=ttyS0 reboot=t panic=1 ${toString cfg.boot.kernelParams}",
      "files": files,
  }, indent=2))
  PY
  mkdir -p $out
  tar -C bundle --sort=name --owner=0 --group=0 --numeric-owner --mtime=@1 -cf - . | gzip -9n > $out/care-engine-${version}-linux-${arch}.tar.gz
  (cd $out && sha256sum care-engine-*.tar.gz > SHA256SUMS)
''
