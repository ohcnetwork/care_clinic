{ pkgs, lib, version }:
pkgs.buildGoModule {
  pname = "care-engine-agent";
  inherit version;
  src = lib.fileset.toSource {
    root = ../agent;
    fileset = lib.fileset.unions [
      (lib.fileset.fileFilter (file: file.hasExt "go") ../agent)
      ../agent/go.mod
      ../agent/go.sum
    ];
  };
  # Update after changing vm/agent/go.mod: build once, copy the "got:" hash.
  vendorHash = "sha256-ARPAhSApRRJ6ufKKxlMZLLVP4M+y/nXdlxwZdAHM92E=";
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" ];
  doCheck = false;
  postInstall = "mv $out/bin/agent $out/bin/care-engine-agent";
}
