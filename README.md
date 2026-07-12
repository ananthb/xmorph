# xmorph

Replaces a running Linux root filesystem with a new in-memory rootfs
built from OCI (Docker) images and rootfs tarballs. The old root is
kept around for inspection and modification.

Works as a Linux rescue environment integrating with systemd's
`rescue.target`. Supports headless mode with Tailscale for SSH access
after the pivot — Tailscale runs in-process via `tsnet`, so the new
rootfs does not need to ship `tailscaled` or `tailscale` binaries.

Coordinates with systemd, OpenRC, and SysVinit to gracefully stop
services before pivoting.

## Principle of Operation

1. Pulls OCI images and/or extracts rootfs tarballs into a RAM-backed tmpfs
2. Merges multiple layers in order (later wins on file conflicts)
3. Coordinates with the init system to stop services
4. Calls `pivot_root(2)` to atomically swap the root filesystem
5. Re-execs itself as `--init` (PID 1 in the new rootfs), brings up
   `tsnet`/SSH if requested, then execs the entrypoint

The old root is accessible at `/mnt/oldroot` by default.

## Installation

### Static binaries

Download from [releases](https://github.com/ananthb/xmorph/releases).
Binaries are pure-Go statics (`CGO_ENABLED=0`) for x86_64, aarch64,
and armv7:

```sh
curl -LO https://github.com/ananthb/xmorph/releases/latest/download/xmorph-x86_64-linux
chmod +x xmorph-x86_64-linux
sudo mv xmorph-x86_64-linux /usr/local/bin/xmorph
```

Each release includes `SHA256SUMS` for verification.

### Nix

```sh
nix run github:ananthb/xmorph -- --help
```

### From source

Requires Go 1.26+:

```sh
CGO_ENABLED=0 go build -o xmorph ./cmd/xmorph
sudo cp xmorph /usr/local/bin/
```

## Usage

### Pivot to a new rootfs

```sh
# Default alpine image
sudo xmorph pivot

# Specific image
sudo xmorph pivot --image ubuntu:22.04

# Local rootfs tarball
sudo xmorph pivot --rootfs ./my-rootfs.tar.gz

# Merge multiple layers (later wins on conflict)
sudo xmorph pivot --image alpine:latest --rootfs ./extra-files/

# Custom entrypoint and command
sudo xmorph pivot --image alpine:latest --entrypoint /bin/sh --command -c --command "echo hello"

# Dry run
sudo xmorph pivot --dry-run
```

### Build an OCI image without pivoting

```sh
# Cache only (pre-warm for fast pivot later)
sudo xmorph build --image alpine:latest

# Write OCI layout to disk
sudo xmorph build --image alpine:latest -o my-image.oci

# Also write a rootfs tarball
sudo xmorph build --image alpine:latest -o my-image.oci --rootfs-output rootfs.tar.gz
```

### Adding files to a public base image

Instead of building a custom image, layer a local rootfs directory over
a public base — xmorph merges them left-to-right at pivot time:

```sh
# overlay/ contains the files you want in the new rootfs (e.g. install.sh,
# etc/config.ign). They land at the same path in the pivoted root.
sudo xmorph pivot --image alpine:latest --rootfs ./overlay/ \
  --entrypoint /install.sh
```

For a fully custom image, build one out-of-band (`podman build`, Nix
`dockerTools`, etc.) and reference it by OCI ref:

```sh
sudo xmorph pivot --image ghcr.io/you/your-installer:tag
```

### Headless mode with Tailscale

For pivoting over SSH without losing your connection:

```sh
sudo xmorph pivot --headless --tailscale.authkey tskey-auth-xxxxx
```

This forks into the background, pivots the root filesystem, and brings
up Tailscale (via `tsnet`) inside the new rootfs. Reconnect via Tailscale
SSH: `ssh root@<hostname>-xmorph`.

Use an [ephemeral auth key](https://tailscale.com/kb/1085/auth-keys) so
the node is automatically removed from your tailnet when xmorph is done.

The `--headless` flag:
- Forks and detaches from the terminal (`setsid`)
- Logs to `/var/log/xmorph.log` (configurable with `--log-dir`)
- Prints the Tailscale hostname and PID before forking
- Implies `--force` (no confirmation prompt)

### systemd rescue.target

xmorph integrates with systemd as a rescue target service.
When the system enters rescue mode, xmorph pivots to the configured rootfs.

A cache warmup service runs during normal boot to pre-pull images,
so the pivot is instant when rescue.target is reached.

#### NixOS module

```nix
{
  inputs.xmorph.url = "github:ananthb/xmorph";

  outputs = { self, nixpkgs, xmorph, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        xmorph.nixosModules.default
        {
          services.xmorph = {
            enable = true;
            package = xmorph.packages.x86_64-linux.default;

            # Images to merge into the rescue rootfs
            images = [ "docker.io/library/alpine:latest" ];

            # Tailscale for SSH access after pivot
            tailscale = {
              enable = true;
              authKeyFile = "/run/secrets/tailscale-key";
            };

            # Pre-warm cache on boot (default: true)
            warmupBuildCache = true;
          };
        }
      ];
    };
  };
}
```

This creates two systemd services:
- `xmorph-cache-warm.service` — runs on `multi-user.target` to pre-pull images
- `xmorph-pivot.service` — runs on `rescue.target` to pivot into the new rootfs

Trigger the pivot with:

```sh
sudo systemctl isolate rescue.target
```

#### Manual systemd setup

Service files are included in the release tarball under
[`init/systemd/`](init/systemd/):

- [`xmorph-pivot.service`](init/systemd/xmorph-pivot.service) — pivots on `rescue.target`
- [`xmorph-cache-warm.service`](init/systemd/xmorph-cache-warm.service) — pre-warms cache on boot

Install them:

```sh
sudo cp init/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable xmorph-pivot.service xmorph-cache-warm.service
```

Edit the pivot service to configure your images and Tailscale auth key:

```sh
sudo systemctl edit xmorph-pivot.service
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/xmorph pivot --systemd-mode --force --image alpine:latest --tailscale.authkey tskey-auth-xxxxx
```

The `--systemd-mode` flag skips init coordination and process termination
(systemd has already stopped services when entering rescue.target).
`CacheDirectory=xmorph` sets `CACHE_DIRECTORY` so xmorph uses
`/var/cache/xmorph` automatically.

## Caching

xmorph caches built rootfs images at the configured cache directory
(default `/var/cache/xmorph`, overridable with `--cache-dir` or the
`CACHE_DIRECTORY` environment variable).

The cache key is derived from the normalized layer list. Repeated
`pivot` or `build` invocations with the same layers skip all image
pulls and layer merging.

Use `xmorph build` with no output flags to pre-warm the cache.

## Requirements

- Linux kernel with `pivot_root` support
- Root privileges (`CAP_SYS_ADMIN`)
- Network access for pulling registry images

## License

Licensed under the terms of the [AGPL-3.0](LICENSE).
