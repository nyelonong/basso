# Devshell for basso.
#
# Provides the native audio libraries the go-mix/mix engine needs to build and
# run on macOS without Homebrew:
#   - SDL2        (go-mix/mix backend)
#   - PortAudio   (audio device I/O)
#   - Go          (compiler / toolchain)
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

  buildInputs = [
    pkgs.SDL2
    pkgs.portaudio
  ];

  # Make the native audio library include/lib dirs discoverable to cgo, since
  # nix-shell does not otherwise inject them into the std environment.
  env.CGO_ENABLED = "1";

  shellHook = ''
    echo "basso devshell"
    echo "  go:        $(go version)"
    echo "  SDL2:      ${pkgs.SDL2}"
    echo "  PortAudio: ${pkgs.portaudio}"
  '';
}