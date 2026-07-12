# Any Linux → NixOS

Replaces the running OS with [NixOS](https://nixos.org/) using
[disko-install](https://github.com/nix-community/disko/blob/master/docs/disko-install.md)
from the `nixos/nix` container image. Your machine config is a flake.

## Trusted source

| Artifact | Source |
|---|---|
| Installer image | `docker.io/nixos/nix:latest` (built by NixOS/Nix maintainers) |
| `disko` / `disko-install` | `github:nix-community/disko` (community-standard, used by nixos-anywhere) |
| Nixpkgs | `github:NixOS/nixpkgs` (your flake pins the revision) |
| Binary cache | `cache.nixos.org` |
| Config | Your own flake, layered in via `--rootfs` |

## Prerequisites

- Outbound HTTPS to `docker.io`, `github.com`, `cache.nixos.org`
- ~2 GiB+ RAM during install (nix evaluation is memory-hungry)
- Target disk ≥ 8 GiB; more if you want a generous /nix/store
- BIOS or UEFI; your disko config decides which bootloader to use

## Files

### `flake.nix`

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    disko.url = "github:nix-community/disko";
    disko.inputs.nixpkgs.follows = "nixpkgs";
  };
  outputs = { self, nixpkgs, disko, ... }: {
    nixosConfigurations.host = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        disko.nixosModules.disko
        ./disko.nix
        ./configuration.nix
      ];
    };
  };
}
```

### `disko.nix` — disk layout (UEFI; swap the bootloader stanza in
`configuration.nix` for BIOS)

```nix
{
  disko.devices.disk.main = {
    type = "disk";
    device = "/dev/sda";
    content = {
      type = "gpt";
      partitions = {
        ESP = {
          size = "512M";
          type = "EF00";
          content = {
            type = "filesystem";
            format = "vfat";
            mountpoint = "/boot";
            mountOptions = [ "umask=0077" ];
          };
        };
        root = {
          size = "100%";
          content = {
            type = "filesystem";
            format = "ext4";
            mountpoint = "/";
          };
        };
      };
    };
  };
}
```

### `configuration.nix` — minimal system

```nix
{ pkgs, ... }: {
  boot.loader.systemd-boot.enable = true;
  boot.loader.efi.canTouchEfiVariables = true;

  networking.hostName = "myhost";
  networking.useDHCP = true;

  services.openssh.enable = true;
  users.users.root.openssh.authorizedKeys.keys = [
    "ssh-ed25519 AAAA... your-key-here"
  ];

  system.stateVersion = "25.05";
}
```

### `install.sh` — entrypoint

```sh
#!/bin/sh
set -eu

nix --extra-experimental-features 'nix-command flakes' \
  run github:nix-community/disko/latest#disko-install -- \
  --flake /flake#host \
  --disk main /dev/sda \
  --write-efi-boot-entries

sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

For a BIOS target, drop `--write-efi-boot-entries`, change the disko
partition layout (bios_grub + ext4 root), and set
`boot.loader.grub = { enable = true; device = "/dev/sda"; }` in
`configuration.nix` instead of systemd-boot.

### Overlay layout

```
overlay/
├── install.sh          # chmod +x
└── flake/
    ├── flake.nix
    ├── disko.nix
    └── configuration.nix
```

## Run it

```sh
sudo xmorph pivot \
  --image docker.io/nixos/nix:latest \
  --rootfs ./overlay/ \
  --entrypoint /install.sh \
  --force
```

The first run will download every store path the flake transitively
needs into the in-RAM `/nix/store`. Size the tmpfs headroom accordingly
— for a minimal NixOS this is ~1-2 GB.

## What happens

1. xmorph pulls `nixos/nix:latest`, merges `./overlay/` (your flake and
   entrypoint) on top, and pivots into the combined rootfs.
2. `disko-install`:
   - Evaluates the flake's `nixosConfigurations.host`
   - Calls `disko` to partition `/dev/sda`, format, and mount under
     `/mnt`
   - Runs `nixos-install` against `/mnt` with the evaluated
     `system.build.toplevel`
   - Installs the bootloader defined in your config (systemd-boot in
     the example above)
   - With `--write-efi-boot-entries`, also adds an `efibootmgr` NVRAM
     entry
3. The script syncs and reboots.
4. Firmware loads systemd-boot; NixOS boots.

## Verify

```sh
ssh root@<host>
nixos-version    # confirms NixOS is running
nixos-rebuild switch --flake /etc/nixos#host   # if you copy the flake to the target
```

## Notes and gotchas

- **`/dev/sda` is hardcoded twice** — once in `disko.nix` and once in
  `install.sh`. They must match.
- **`system.stateVersion`** should match the nixpkgs branch you're
  installing from; it's a stability anchor, not a free-form version.
- **Binary cache hit rate**: if your config pulls a lot from outside
  nixpkgs, expect long builds during the install. For fleet use,
  consider a local cache (`extra-substituters`).
- **nixos-anywhere is the alternative**: if you want to *push* an
  install from your laptop over SSH instead of pivot-installing locally,
  use [nixos-anywhere](https://github.com/nix-community/nixos-anywhere)
  — its model is "SSH in + kexec," which is a different shape than
  xmorph's pivot-in-place model. Both end up running disko +
  nixos-install; pick by where you'd rather drive from.
- **State volumes**: the example partitions the whole disk. For setups
  where `/home` or `/var` lives on a separate disk you want to keep,
  scope `disko.devices.disk` accordingly.

## Source docs

- [disko-install](https://github.com/nix-community/disko/blob/master/docs/disko-install.md)
- [disko reference](https://github.com/nix-community/disko/blob/master/docs/reference.md)
- [NixOS manual: installation](https://nixos.org/manual/nixos/stable/index.html#sec-installation)
- [nixos-anywhere](https://github.com/nix-community/nixos-anywhere) (alternative)
