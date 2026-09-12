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
          latex = pkgs.texliveFull;
          go = pkgs.buildGoModule {
            pname = "enceladus-go";
            version = "0.0.0";
            src = pkgs.lib.cleanSourceWith {
              src = ./go;
              name = "enceladus-go-src";
            };
            vendorHash = "sha256-yvTFVjhJH5Hu6y/GfB+TypNjae/tmXoPCARbpwJOyy0=";
            subPackages = [
              "./cmd/enceladus-api"
              "./cmd/fetch-sim-archives"
              "./cmd/fetch-population"
            ];
            ldflags = [ "-s" "-w" ];
          };
          api = pkgs.writeShellApplication {
            name = "enceladus-api";
            runtimeInputs = [ latex pkgs.pandoc r go ];
            text = ''
              export HOME="''${HOME:-/root}"
              export ENCELADUS_SOURCE_ROOT="''${ENCELADUS_SOURCE_ROOT:-${self}/src}"
              export ENCELADUS_HOME="''${ENCELADUS_HOME:-$PWD/.enceladus}"
              export ENCELADUS_FETCH_SIM_BIN="''${ENCELADUS_FETCH_SIM_BIN:-enceladus-fetch-sim-archives}"
              mkdir -p "$ENCELADUS_HOME"
              exec enceladus-api
            '';
          };
          population = pkgs.writeShellApplication {
            name = "enceladus-population";
            runtimeInputs = [ go ];
            text = ''
              export HOME="''${HOME:-/root}"
              export ENCELADUS_HOME="''${ENCELADUS_HOME:-$PWD/.enceladus}"
              export ENCELADUS_POPULATION_DATA_PATH="''${ENCELADUS_POPULATION_DATA_PATH:-$ENCELADUS_HOME/data/population.csv}"
              export ENCELADUS_DATASUS_MAX_YEAR_PATH="''${ENCELADUS_DATASUS_MAX_YEAR_PATH:-$ENCELADUS_HOME/data/datasus-max-year.txt}"
              mkdir -p "$ENCELADUS_HOME/data"
              exec enceladus-fetch-population
            '';
          };
        in { inherit api go latex pkgs population r; };
    in {
      apps = forAllSystems (system:
        let env = forSystem system;
        in {
          default = {
            type = "app";
            program = "${env.api}/bin/enceladus-api";
          };
          population = {
            type = "app";
            program = "${env.population}/bin/enceladus-population";
          };
        });
      packages = forAllSystems (system:
        let env = forSystem system;
        in {
          default = env.api;
          api = env.api;
          go = env.go;
          population = env.population;
          redis = env.pkgs.redis;
        });
      devShells = forAllSystems (system:
        let env = forSystem system;
        in {
          default = env.pkgs.mkShell {
            packages = [ env.latex env.pkgs.pandoc env.go env.r ]
              ++ env.pkgs.lib.optional (!env.pkgs.stdenv.isDarwin) env.pkgs.redis;
            shellHook = ''
              export LANG=en_US.UTF-8
              unset LC_COLLATE
              export ENCELADUS_SOURCE_ROOT="$PWD/src"
              export ENCELADUS_FETCH_SIM_BIN="enceladus-fetch-sim-archives"
              export ENCELADUS_HOME="''${ENCELADUS_HOME:-$PWD/.enceladus}"
              mkdir -p "$ENCELADUS_HOME"
            '';
          };
        });
    };
}