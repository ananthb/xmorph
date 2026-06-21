{
  description = "xmorph - Linux pivot_root tool for OCI images";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    git-hooks = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, git-hooks }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;

        version = if (self ? shortRev) then self.shortRev else "dev";

        # vendorHash is populated by `nix build` on first run — it will print
        # the expected hash and you paste it here. lib.fakeHash forces that
        # error on first build; once set, the value is reproducible.
        vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

        # Build a static xmorph for a given GOARCH (+ optional GOARM).
        mkXmorph = goarch: goarm: pkgs.buildGoModule {
          pname = "xmorph";
          inherit version;
          src = ./.;
          inherit vendorHash;
          env = {
            CGO_ENABLED = "0";
            GOOS = "linux";
            GOARCH = goarch;
          } // lib.optionalAttrs (goarm != "") { GOARM = goarm; };
          subPackages = [ "cmd/xmorph" ];
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          # Tests run in CI via `go test`; the nix sandbox can't exercise
          # the pivot/mount paths anyway.
          doCheck = false;
          meta = with lib; {
            description = "Linux pivot_root tool for OCI images";
            homepage = "https://github.com/ananthb/xmorph";
            license = licenses.agpl3Only;
            platforms = platforms.linux;
            mainProgram = "xmorph";
          };
        };

        xmorph-x86_64 = mkXmorph "amd64" "";
        xmorph-aarch64 = mkXmorph "arm64" "";
        xmorph-armv7 = mkXmorph "arm" "7";

        mkReleaseTarball = name: xmorphBuild: pkgs.stdenv.mkDerivation {
          pname = "xmorph-release-${name}";
          inherit version;
          src = xmorphBuild;

          nativeBuildInputs = [ pkgs.gnutar pkgs.gzip ];

          buildPhase = ''
            mkdir -p xmorph/bin
            cp $src/bin/xmorph xmorph/bin/
            cp -r ${./.}/init xmorph/ 2>/dev/null || true
            cp ${./.}/README.md xmorph/ 2>/dev/null || echo "No README" > xmorph/README.md
            cp ${./.}/LICENSE xmorph/ 2>/dev/null || true
          '';

          installPhase = ''
            mkdir -p $out
            tar -czvf $out/xmorph-${name}.tar.gz xmorph
          '';
        };

        releaseTarball-x86_64 = mkReleaseTarball "x86_64-linux" xmorph-x86_64;
        releaseTarball-aarch64 = mkReleaseTarball "aarch64-linux" xmorph-aarch64;
        releaseTarball-armv7 = mkReleaseTarball "armv7-linux" xmorph-armv7;

        pre-commit = git-hooks.lib.${system}.run {
          src = ./.;
          hooks = {
            check-merge-conflicts.enable = true;
            check-toml.enable = true;
            check-yaml.enable = true;
            detect-private-keys.enable = true;
            end-of-file-fixer.enable = true;
            trim-trailing-whitespace.enable = true;
            gofmt.enable = true;
            # govet runs against the host OS; this is a Linux-only project
            # so it would fail on darwin. CI runs `go vet` for real.
          };
        };

      in
      {
        packages = {
          default = xmorph-x86_64;
          xmorph = xmorph-x86_64;

          inherit xmorph-x86_64 xmorph-aarch64 xmorph-armv7;

          releaseTarball = releaseTarball-x86_64;
          inherit releaseTarball-x86_64 releaseTarball-aarch64 releaseTarball-armv7;

          # Build all platforms (writes binaries to ./dist/)
          build-all = pkgs.writeShellScriptBin "xmorph-build-all" ''
            set -e
            rm -rf dist
            mkdir -p dist
            echo "Building x86_64..."
            cp ${xmorph-x86_64}/bin/xmorph dist/xmorph-x86_64-linux
            echo "Building aarch64..."
            cp ${xmorph-aarch64}/bin/xmorph dist/xmorph-aarch64-linux
            echo "Building armv7..."
            cp ${xmorph-armv7}/bin/xmorph dist/xmorph-armv7-linux
            echo ""
            echo "Binaries:"
            ls -lh dist/
          '';

          # Combined release: all three tarballs + SHA256SUMS
          release = pkgs.runCommand "xmorph-${version}-release" {
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
          build = xmorph-x86_64;
          build-aarch64 = xmorph-aarch64;
          build-armv7 = xmorph-armv7;

          test = pkgs.buildGoModule {
            pname = "xmorph-test";
            inherit version vendorHash;
            src = ./.;
            env.CGO_ENABLED = "0";
            doCheck = true;
            # buildGoModule's default check runs `go test ./...`. We only
            # need the checkPhase output — discard the install artifact.
            installPhase = "touch $out";
          };

          fmt = pkgs.runCommand "xmorph-fmt" {
            nativeBuildInputs = [ pkgs.go ];
          } ''
            cd ${./.}
            unformatted=$(gofmt -l .)
            if [ -n "$unformatted" ]; then
              echo "gofmt would reformat:"
              echo "$unformatted"
              exit 1
            fi
            touch $out
          '';

          # NixOS VM test: local rootfs build + cache
          nixos-local = pkgs.testers.nixosTest {
            name = "xmorph-local-build";

            nodes.machine = { pkgs, lib, ... }: {
              imports = [ self.nixosModules.default ];

              services.xmorph = {
                enable = true;
                package = xmorph-x86_64;
                images = [ ];
                warmupBuildCache = true;
              };

              # Create a minimal rootfs tarball with busybox
              systemd.services.xmorph-test-rootfs = {
                description = "Create test rootfs for xmorph";
                wantedBy = [ "multi-user.target" ];
                before = [ "xmorph-cache-warm.service" ];
                path = [ pkgs.gnutar pkgs.gzip ];
                serviceConfig = {
                  Type = "oneshot";
                  RemainAfterExit = true;
                };
                script = ''
                  mkdir -p /tmp/xmorph-test-rootfs/{bin,sbin,lib,dev,proc,sys,tmp,etc,var,run}
                  cp ${pkgs.pkgsStatic.busybox}/bin/busybox /tmp/xmorph-test-rootfs/bin/busybox
                  ln -sf busybox /tmp/xmorph-test-rootfs/bin/sh
                  ln -sf /bin/sh /tmp/xmorph-test-rootfs/sbin/init
                  echo "xmorph-test" > /tmp/xmorph-test-rootfs/etc/hostname
                  mkdir -p /var/lib/xmorph-test
                  tar czf /var/lib/xmorph-test/rootfs.tar.gz -C /tmp/xmorph-test-rootfs .
                '';
              };

              systemd.services.xmorph-cache-warm.serviceConfig.ExecStart =
                lib.mkForce "${xmorph-x86_64}/bin/xmorph build --rootfs /var/lib/xmorph-test/rootfs.tar.gz";

              virtualisation.memorySize = 2048;
            };

            testScript = ''
              machine.wait_for_unit("multi-user.target")
              machine.wait_for_unit("xmorph-test-rootfs.service")
              machine.succeed("test -f /var/lib/xmorph-test/rootfs.tar.gz")
              machine.wait_for_unit("xmorph-cache-warm.service")
            '';
          };

          # NixOS VM test: headscale integration (offline, ~2-3 min)
          nixos-headscale = import ./nix/tests/headscale.nix {
            inherit pkgs;
            lib = pkgs.lib;
            xmorph-package = xmorph-x86_64;
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            golangci-lint
            less
          ];

          shellHook = ''
            ${pre-commit.shellHook}
            export PAGER="${pkgs.less}/bin/less"
            echo "xmorph development environment"
            echo "Go version: $(go version)"
            echo ""
            echo "Commands:"
            echo "  go build ./...         # build everything"
            echo "  go test ./...          # run unit tests"
            echo "  nix run .#build-all    # cross-compile to dist/"
            echo "  nix build .#release    # release tarballs + SHA256SUMS"
            echo "  nix flake check        # all checks + NixOS VM tests"
          '';
        };
      }
    ) // {
      # NixOS module for xmorph rescue pivot
      nixosModules.default = import ./nix/module.nix;
      nixosModules.xmorph = import ./nix/module.nix;
    };
}
