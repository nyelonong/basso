# Devshell for basso.
#
# Pure-Go build: github.com/gopxl/beep/v2's audio backend (ebitengine/oto,
# via ebitengine/purego) talks to the OS audio API directly through
# dlopen-based FFI, no cgo. The only devshell dependency is Go itself;
# confirmed the binary builds and runs correctly with CGO_ENABLED=0.
#
# Enter with:  nix-shell
# Build with:  nix-shell --run 'go build ./...'
#
# nixpkgs is resolved via $NIX_PATH / the Determinate `extra-nix-path` setting
# (nixpkgs=flake:https://flakehub.com/f/DeterminateSystems/nixpkgs-weekly/*.tar.gz).
# Override locally by exporting NIX_PATH=nixpkgs=/path/to/nixpkgs.

{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  name = "basso-devshell";

  nativeBuildInputs = [
    pkgs.go
  ];

  shellHook = ''
    echo "basso devshell"
    echo "  go: $(go version)"
  '';
}
