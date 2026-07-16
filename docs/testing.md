# Testing

xmorph has three tiers of tests, matching how much of a real Linux kernel
each one needs.

## 1. Unit tests + lint (any OS)

```sh
nix run .#build      # gofmt + go vet (GOOS=linux) + go test
```

On Linux this runs `go test ./...`; on macOS it runs the pure-Go tests
natively and cross-compiles the rest. This is what the `build` CI job runs.

Tests that need root and mount namespaces (`internal/pivot`, and the
as-root tar-extraction cases in `internal/oci`) are guarded with
`if os.Geteuid() != 0 { t.Skip(...) }`, so they skip here rather than fail.

## 2. Integration tests on a real kernel (CI)

The pivot path — `pivot_root(2)`, mount ordering, tar extraction running as
root — can only be exercised against a real kernel with `CAP_SYS_ADMIN`.
The nix build sandbox can't do mounts, so the integration test binaries are
compiled by the `xmorphIntegrationTests` derivation and run inside a NixOS
VM test:

```sh
nix build -L .#checks.x86_64-linux.nixos-pivot   # pivot_root + extraction
nix flake check                                  # all checks + every VM test
```

The `integration` CI job runs `nixos-pivot` on every push and PR (GitHub's
Linux runners expose `/dev/kvm`). This is the project's real-kernel
regression coverage — notably it guards the mount-ordering invariant that
`/proc`, `/sys`, and `/dev` stay visible after the pivot.

## 3. Fast real-kernel iteration on macOS (developer option)

NixOS VM tests are the source of truth, but they're slow to iterate on. On
Apple Silicon you can run the same Linux tests directly against a real
kernel using [Apple `container`](https://github.com/apple/container), which
puts each container in its own lightweight VM.

One-time setup:

```sh
container system start
container system kernel set --recommended
```

Then run any package's tests as root with mount capabilities:

```sh
container run --rm --cap-add ALL \
  -v "$PWD:/src" \
  -v "$HOME/go/pkg/mod:/go/pkg/mod:ro" \
  -w /src \
  docker.io/library/golang:1.26 \
  sh -c 'GOFLAGS=-mod=mod GOPROXY=off go test -race ./...'
```

`--cap-add ALL` grants the `CAP_SYS_ADMIN` that `unshare`/`pivot_root`
require; mounting `$HOME/go/pkg/mod` read-only reuses your module cache so
the run is offline and fast. This is a convenience for local development —
CI relies on the NixOS VM test in tier 2, not on `container`.
