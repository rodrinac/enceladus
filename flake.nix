{
  description = "Reproducible Enceladus runtime for local development and Linux hosts";

  inputs.nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst";

  outputs = { self, nixpkgs, ... }:
    let
      systems = [ "aarch64-darwin" "x86_64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      forSystem = system:
        let
          pkgs = import nixpkgs {
            inherit system;
            config.permittedInsecurePackages = [ "electron-41.10.6" ];
          };
          rPackages = pkgs.rPackages;
          microdatasus = rPackages.buildRPackage {
            pname = "microdatasus";
            version = "3.0.0-7109ec2";
            src = pkgs.fetchFromGitHub {
              owner = "rfsaldanha";
              repo = "microdatasus";
              rev = "7109ec2c42cf674ba453e0d7a20d2f464890b543";
              hash = "sha256-ZB6YTGqDOE2Tcg7da0Utded5XpKZQFso5wL6H/GrdB8=";
            };
            propagatedBuildInputs = with rPackages; [
              checkmate cli curl data_table dplyr foreign magrittr rlang stringi tibble zip
            ];
          };
          r = pkgs.rWrapper.override {
            packages = with rPackages; [
              dplyr ggplot2 gridExtra janitor kableExtra matrixStats readr rmarkdown tidyr microdatasus
            ];
          };
          python = pkgs.python312.withPackages (ps: [
            ps.boto3 ps.hypercorn ps."quart-cors" ps.quart ps.pyyaml ps.redis
          ]);
          latex = pkgs.texliveSmall;
          api = pkgs.writeShellApplication {
            name = "enceladus-api";
            runtimeInputs = [ latex pkgs.pandoc python r ];
            text = ''
              export PYTHONPATH=${self}/src''${PYTHONPATH:+:$PYTHONPATH}
              export ENCELADUS_HOME="''${ENCELADUS_HOME:-$PWD/.enceladus}"
              mkdir -p "$ENCELADUS_HOME"
              exec hypercorn --bind "''${ENCELADUS_BIND:-0.0.0.0:8000}" main:app
            '';
          };
        in { inherit api latex pkgs python r; };
    in {
      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${(forSystem system).api}/bin/enceladus-api";
        };
      });
      packages = forAllSystems (system: {
        default = (forSystem system).api;
      });
      devShells = forAllSystems (system:
        let env = forSystem system;
        in {
          default = env.pkgs.mkShell {
            packages = [ env.latex env.pkgs.pandoc env.python env.r ];
            shellHook = ''
              export LANG=en_US.UTF-8
              unset LC_COLLATE
              export PYTHONPATH="$PWD/src''${PYTHONPATH:+:$PYTHONPATH}"
              export ENCELADUS_HOME="''${ENCELADUS_HOME:-$PWD/.enceladus}"
              mkdir -p "$ENCELADUS_HOME"
            '';
          };
        });
    };
}
