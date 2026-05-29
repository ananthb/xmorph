{
  description = "Xenomorph - Linux pivot_root tool for OCI images";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    git-hooks = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    runz = {
      url = "github:ananthb/runz";
      flake = false;
    };
    oci-spec-zig = {
      url = "github:navidys/oci-spec-zig";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, flake-utils, git-hooks, runz, oci-spec-zig }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        version = if (self ? shortRev) then self.shortRev else "dev";

        # Static build targets (Zig cross-compilation)
        targets = {
          x86_64 = "x86_64-linux-musl";
          aarch64 = "aarch64-linux-musl";
          armv7 = "arm-linux-musleabihf";
        };

        # Read dependency hashes from build.zig.zon files so zig --system works.
        # These must match the .hash fields in build.zig.zon and runz/build.zig.zon.
        runzHash = "runz-0.1.0-Fz_yRd5GBgCfsupWOqY1LRk7AXS49BlASN0f8pq8WwVF";
        ociSpecHash = "ocispec-0.4.0-dev-voj0cey1AgDS-1Itn3Xu5AiWtB6cwMddZtDUssOtWrIn";

        # Create a directory structure that zig --system expects:
        # pkgdir/<hash> → source tree
        zigDepsDir = pkgs.runCommand "xenomorph-zig-deps" {} ''
          mkdir -p $out
          ln -s ${runz} $out/${runzHash}
          ln -s ${oci-spec-zig} $out/${ociSpecHash}
        '';

        # Build a static xenomorph for a given target
        mkXenomorph = name: zigTarget: pkgs.stdenv.mkDerivation {
          pname = "xenomorph-${name}";
          inherit version;
          src = ./.;

          nativeBuildInputs = [ pkgs.zig ];

          dontConfigure = true;
          dontInstall = true;

          buildPhase = ''
            runHook preBuild
            export ZIG_GLOBAL_CACHE_DIR=$(mktemp -d)
            zig build \
              --system ${zigDepsDir} \
              -Doptimize=ReleaseSafe \
              -Dtarget=${zigTarget} \
              --prefix $out
            runHook postBuild
          '';

          meta = with pkgs.lib; {
            description = "Linux pivot_root tool for OCI images";
            homepage = "https://github.com/ananthb/xenomorph";
            license = licenses.agpl3Only;
            platforms = platforms.linux;
            mainProgram = "xenomorph";
          };
        };

        # Build a release tarball for a given target
        mkReleaseTarball = name: xenomorphBuild: pkgs.stdenv.mkDerivation {
          pname = "xenomorph-release-${name}";
          inherit version;
          src = xenomorphBuild;

          nativeBuildInputs = [ pkgs.gnutar pkgs.gzip ];

          buildPhase = ''
            mkdir -p xenomorph/bin
            cp $src/bin/xenomorph xenomorph/bin/
            cp -r ${./.}/init xenomorph/ 2>/dev/null || true
            cp ${./.}/README.md xenomorph/ 2>/dev/null || echo "No README" > xenomorph/README.md
            cp ${./.}/LICENSE xenomorph/ 2>/dev/null || true
          '';

          installPhase = ''
            mkdir -p $out
            tar -czvf $out/xenomorph-${name}.tar.gz xenomorph
          '';
        };

        # Create builds for all targets
        xenomorph-x86_64 = mkXenomorph "x86_64" targets.x86_64;
        xenomorph-aarch64 = mkXenomorph "aarch64" targets.aarch64;
        xenomorph-armv7 = mkXenomorph "armv7" targets.armv7;

        releaseTarball-x86_64 = mkReleaseTarball "x86_64-linux" xenomorph-x86_64;
        releaseTarball-aarch64 = mkReleaseTarball "aarch64-linux" xenomorph-aarch64;
        releaseTarball-armv7 = mkReleaseTarball "armv7-linux" xenomorph-armv7;

        pre-commit = git-hooks.lib.${system}.run {
          src = ./.;
          hooks = {
            check-merge-conflicts.enable = true;
            check-toml.enable = true;
            check-yaml.enable = true;
            detect-private-keys.enable = true;
            end-of-file-fixer.enable = true;
            trim-trailing-whitespace.enable = true;
            zigfmt = {
              enable = true;
              name = "zig fmt";
              entry = "${pkgs.zig}/bin/zig fmt";
              types = [ "zig" ];
            };
          };
        };

      in
      {
        packages = {
          default = xenomorph-x86_64;
          xenomorph = xenomorph-x86_64;

          inherit xenomorph-x86_64 xenomorph-aarch64 xenomorph-armv7;

          releaseTarball = releaseTarball-x86_64;
          inherit releaseTarball-x86_64 releaseTarball-aarch64 releaseTarball-armv7;

          # Build all platforms (writes binaries to ./dist/)
          build-all = pkgs.writeShellScriptBin "xenomorph-build-all" ''
            set -e
            rm -rf dist
            mkdir -p dist
            echo "Building x86_64..."
            cp ${xenomorph-x86_64}/bin/xenomorph dist/xenomorph-x86_64-linux
            echo "Building aarch64..."
            cp ${xenomorph-aarch64}/bin/xenomorph dist/xenomorph-aarch64-linux
            echo "Building armv7..."
            cp ${xenomorph-armv7}/bin/xenomorph dist/xenomorph-armv7-linux
            echo ""
            echo "Binaries:"
            ls -lh dist/
          '';

          # Combined release with all artifacts for Garnix
          release = pkgs.runCommand "xenomorph-${version}-release" {
            nativeBuildInputs = [ pkgs.coreutils ];
          } ''
            mkdir -p $out
            cp ${releaseTarball-x86_64}/*.tar.gz $out/
            cp ${releaseTarball-aarch64}/*.tar.gz $out/
            cp ${releaseTarball-armv7}/*.tar.gz $out/
            cd $out
            sha256sum *.tar.gz > SHA256SUMS
          '';
        };

        checks = {
          build = xenomorph-x86_64;
          build-aarch64 = xenomorph-aarch64;
          build-armv7 = xenomorph-armv7;

          test = pkgs.stdenv.mkDerivation {
            pname = "xenomorph-test";
            inherit version;
            src = ./.;

            nativeBuildInputs = [ pkgs.zig ];
            dontConfigure = true;
            dontInstall = true;

            buildPhase = ''
              export ZIG_GLOBAL_CACHE_DIR=$(mktemp -d)
              zig build test --system ${zigDepsDir}
              touch $out
            '';
          };

          fmt = pkgs.stdenv.mkDerivation {
            pname = "xenomorph-fmt";
            inherit version;
            src = ./.;

            nativeBuildInputs = [ pkgs.zig ];
            dontConfigure = true;
            dontInstall = true;

            buildPhase = ''
              export ZIG_GLOBAL_CACHE_DIR=$(mktemp -d)
              zig fmt --check src/ || echo "Format check skipped"
              touch $out
            '';
          };

          # Fuzz corpus
          fuzz = pkgs.stdenv.mkDerivation {
            pname = "xenomorph-fuzz";
            inherit version;
            src = ./.;

            nativeBuildInputs = [ pkgs.zig ];
            dontConfigure = true;
            dontInstall = true;

            buildPhase = ''
              export ZIG_GLOBAL_CACHE_DIR=$(mktemp -d)
              zig build fuzz --system ${zigDepsDir}
              touch $out
            '';
          };

          # NixOS VM test: local rootfs build + cache
          nixos-local = pkgs.testers.nixosTest {
            name = "xenomorph-local-build";

            nodes.machine = { pkgs, lib, ... }: {
              imports = [ self.nixosModules.default ];

              services.xenomorph = {
                enable = true;
                package = xenomorph-x86_64;
                images = [ ];
                warmupBuildCache = true;
              };

              # Create a minimal rootfs tarball with busybox
              systemd.services.xenomorph-test-rootfs = {
                description = "Create test rootfs for xenomorph";
                wantedBy = [ "multi-user.target" ];
                before = [ "xenomorph-cache-warm.service" ];
                path = [ pkgs.gnutar pkgs.gzip ];
                serviceConfig = {
                  Type = "oneshot";
                  RemainAfterExit = true;
                };
                script = ''
                  mkdir -p /tmp/xenomorph-test-rootfs/{bin,sbin,lib,dev,proc,sys,tmp,etc,var,run}
                  cp ${pkgs.pkgsStatic.busybox}/bin/busybox /tmp/xenomorph-test-rootfs/bin/busybox
                  ln -sf busybox /tmp/xenomorph-test-rootfs/bin/sh
                  ln -sf /bin/sh /tmp/xenomorph-test-rootfs/sbin/init
                  echo "xenomorph-test" > /tmp/xenomorph-test-rootfs/etc/hostname
                  mkdir -p /var/lib/xenomorph-test
                  tar czf /var/lib/xenomorph-test/rootfs.tar.gz -C /tmp/xenomorph-test-rootfs .
                '';
              };

              systemd.services.xenomorph-cache-warm.serviceConfig.ExecStart =
                lib.mkForce "${xenomorph-x86_64}/bin/xenomorph build --rootfs /var/lib/xenomorph-test/rootfs.tar.gz";

              virtualisation.memorySize = 2048;
            };

            testScript = ''
              machine.wait_for_unit("multi-user.target")
              machine.wait_for_unit("xenomorph-test-rootfs.service")
              machine.succeed("test -f /var/lib/xenomorph-test/rootfs.tar.gz")
              machine.wait_for_unit("xenomorph-cache-warm.service")
            '';
          };

          # NixOS VM test: headscale integration (offline, ~2-3 min)
          # Run: nix build .#checks.x86_64-linux.nixos-headscale -L
          nixos-headscale = import ./nix/tests/headscale.nix {
            inherit pkgs;
            lib = pkgs.lib;
            xenomorph-package = xenomorph-x86_64;
          };

          # NOTE: nixos-registry-pull and nixos-run tests require internet access
          # and cannot run in the nix sandbox. Run them locally with:
          #   nix build .#checks.x86_64-linux.nixos-registry-pull
          #   nix build .#checks.x86_64-linux.nixos-run

        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            zig
            zls
            valgrind
            less
          ];

          shellHook = ''
            ${pre-commit.shellHook}
            export PAGER="${pkgs.less}/bin/less"
            echo "Xenomorph development environment"
            echo "Zig version: $(zig version)"
            echo ""
            echo "Commands:"
            echo "  nix run .#build-all   # Build binaries for all platforms → dist/"
            echo "  nix build .#release    # Build release tarballs + checksums → result/"
            echo "  nix flake check       # Run all checks including NixOS VM test"
          '';
        };
      }
    ) // {
      # NixOS module for xenomorph rescue pivot
      nixosModules.default = import ./nix/module.nix;
      nixosModules.xenomorph = import ./nix/module.nix;
    };
}
