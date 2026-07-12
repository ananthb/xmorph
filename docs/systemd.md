# systemd integration

xmorph runs as a `rescue.target` unit: `systemctl isolate rescue.target`
tears down normal services and pivots into the configured rootfs. A
companion cache-warm unit runs on `multi-user.target` so the pivot
itself is sub-second when you actually need it.

## NixOS module

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

            images = [ "docker.io/library/alpine:latest" ];

            tailscale = {
              enable = true;
              authKeyFile = "/run/secrets/tailscale-key";
            };

            warmupBuildCache = true; # default
          };
        }
      ];
    };
  };
}
```

The module installs two services:

- `xmorph-cache-warm.service` — runs on `multi-user.target` to pre-pull images
- `xmorph-pivot.service` — runs on `rescue.target` to pivot into the new rootfs

Trigger with `sudo systemctl isolate rescue.target`.

## Manual setup

Service files ship in the release tarball under
[`init/systemd/`](../init/systemd/):

- `xmorph-pivot.service` — pivots on `rescue.target`
- `xmorph-cache-warm.service` — pre-warms cache on boot

Install:

```sh
sudo cp init/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable xmorph-pivot.service xmorph-cache-warm.service
```

Edit the pivot service to configure images and Tailscale:

```sh
sudo systemctl edit xmorph-pivot.service
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/xmorph pivot --systemd-mode --force \
  --image alpine:latest \
  --tailscale.authkey tskey-auth-xxxxx
```

`--systemd-mode` skips init coordination and process termination (systemd
already stopped services when entering `rescue.target`). The unit's
`CacheDirectory=xmorph` sets `CACHE_DIRECTORY`, so xmorph uses
`/var/cache/xmorph` automatically.

## Caching

xmorph caches built rootfs images at the configured cache directory
(default `/var/cache/xmorph`; override with `--cache-dir` or
`CACHE_DIRECTORY`). The cache key is derived from the normalized layer
list — repeated `pivot` or `build` calls with the same layers skip
image pulls and layer merges entirely. Pre-warm with `xmorph build`
(no output flags).
