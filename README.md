# xmorph

Replaces a running Linux root filesystem with a new in-memory rootfs
built from OCI (Docker) images and rootfs tarballs — without rebooting.
The old root is kept for inspection and modification at `/mnt/oldroot`.

Useful as a rescue environment for hosts you can't reboot on demand, a
reprovisioning tool for pivoting into a fresh OS, or a `rescue.target`
replacement that stays reachable over Tailscale.

## Install

```sh
# Nix
nix run github:ananthb/xmorph -- --help

# Static binary (each release ships a SHA256SUMS file alongside the archives)
curl -LO https://github.com/ananthb/xmorph/releases/latest/download/xmorph-x86_64-linux.tar.gz
tar xzf xmorph-x86_64-linux.tar.gz
sudo mv xmorph/xmorph /usr/local/bin/xmorph

# From source (Go 1.26+)
CGO_ENABLED=0 go build -o xmorph ./cmd/xmorph
```

## Docs

Full guide, flag reference, systemd + NixOS integration, and
reprovisioning recipes at **[ananthb.github.io/xmorph](https://ananthb.github.io/xmorph)**.

- [Rescue an unreachable host](docs/rescue.md)
- [Reprovision to Flatcar / FCOS / Alpine / NixOS / Ubuntu](docs/reprovision/)

Licensed under the [AGPL-3.0](LICENSE).
