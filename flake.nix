{
  description = "Buildkite agent release binary";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { nixpkgs, ... }:
    let
      release = builtins.fromJSON (builtins.readFile ./nix/release.json);
      inherit (release) version;
      platforms = {
        x86_64-linux = "linux-amd64";
        aarch64-linux = "linux-arm64";
        x86_64-darwin = "darwin-amd64";
        aarch64-darwin = "darwin-arm64";
      };
    in {
      packages = builtins.mapAttrs (system: platform:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in rec {
          default = buildkite-agent;
          buildkite-agent = pkgs.stdenvNoCC.mkDerivation {
            pname = "buildkite-agent";
            inherit version;
            src = pkgs.fetchurl {
              url = "https://github.com/buildkite/agent/releases/download/v${version}/buildkite-agent-${platform}-${version}.tar.gz";
              sha256 = release.sha256.${platform};
            };

            dontUnpack = true;
            dontStrip = true;
            installPhase = ''
              runHook preInstall
              tar -xzf "$src" ./buildkite-agent
              install -Dm755 buildkite-agent "$out/bin/buildkite-agent"
              runHook postInstall
            '';

            meta.mainProgram = "buildkite-agent";
          };
        }
      ) platforms;
    };
}
