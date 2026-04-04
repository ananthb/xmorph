# Tests

## Unit tests

```sh
zig build test
```

Runs inline tests from `src/` and the test suite in `tests/unit/main.zig`.
Covers config parsing, image reference normalization, OCI layout writing,
init script generation, and module compilation.

## Fuzz tests

```sh
zig build fuzz          # run seed corpus once (CI-safe)
zig build fuzz -ffuzz   # continuous fuzzing with input mutation
```

Fuzz targets in `tests/fuzz.zig`:
- Image reference normalizer
- Layer deduplication
- Build cache key computation

## Valgrind

```sh
zig build valgrind
```

Runs the unit test suite under valgrind with `--leak-check=full`.
Requires valgrind (available in `nix develop`).

## NixOS VM tests

### Local (offline, runs in CI)

```sh
nix build .#checks.x86_64-linux.nixos-local
```

Boots a NixOS VM, creates a busybox rootfs tarball, runs
`xenomorph build --rootfs`, and verifies the cache warmup service.

### Registry pull (requires internet, manual only)

```sh
nix build .#checks.x86_64-linux.nixos-registry-pull
```

Pulls `alpine:latest` from Docker Hub inside a NixOS VM. Tests the
registry HTTP client, image size estimation, and build cache.
Cannot run in the nix sandbox (no network).

### RUN support (requires internet, manual only)

```sh
nix build .#checks.x86_64-linux.nixos-run
```

Tests Containerfile `RUN` execution and `--ssh-port` (dropbear install
via `apk add`). Cannot run in the nix sandbox (no network).

### Headscale integration (offline)

```sh
nix build .#checks.x86_64-linux.nixos-headscale -L
```

Boots two NixOS VMs: a Headscale server and a xenomorph node. Creates a
pre-auth key, runs `xenomorph pivot --contain` with tailscale flags pointing
at the local Headscale instance, and verifies the node connects to the Tailnet.
Tests the full config pipeline end-to-end. Runs offline (tailscale binaries
from nixpkgs, no registry pull).

## CI

`nix flake check` runs: build (x86_64, aarch64, armv7), test, fuzz,
fmt, and nixos-local. The network-dependent tests are excluded.
