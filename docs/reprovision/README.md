# Reprovisioning with xmorph

xmorph is normally pitched as a rescue tool — pivot from a working OS into a
rescue rootfs in RAM so you can fix the disks. The same primitive lets you
**replace the OS entirely**: pivot into an installer rootfs in RAM, let it
partition the host's disks and lay down a new OS, then reboot.

The recipes in this directory take you from **any Linux** the xmorph binary
can run on, to:

| Target | Installer image (public, upstream) | Config language | Page |
|---|---|---|---|
| Fedora CoreOS | `quay.io/coreos/coreos-installer:release` | Ignition | [fcos.md](fcos.md) |
| Flatcar | `docker.io/alpine` + `flatcar-install` script from `github.com/flatcar/init` | Ignition | [flatcar.md](flatcar.md) |
| NixOS | `docker.io/nixos/nix` + `nix-community/disko` | Nix flake + disko | [nixos.md](nixos.md) |
| Ubuntu 24.04 | `docker.io/debian:stable` + `debootstrap` | cloud-init NoCloud | [ubuntu.md](ubuntu.md) |
| Alpine | `docker.io/alpine` + `alpine-conf` | answerfile | [alpine.md](alpine.md) |

We do not build or host any of these images. Every recipe pulls from the
upstream registry and (where applicable) downloads the installer's payload
from the project's own release infrastructure.

## What xmorph does and does not do

xmorph does:

- Pull an OCI image (or build one from a Containerfile)
- Move it into a tmpfs in RAM
- Coordinate with the init system to stop services
- `pivot_root` into the in-RAM rootfs
- Exec the entrypoint, which is your installer

xmorph does not:

- Partition disks (the installer does)
- Validate your installer config (the installer does, after pivot)
- Resume on failure (a failed installer leaves you with an unbootable disk;
  recovery means a serial console, IPMI, or physical access)

## Shared safety notes

- **The pivot is one-way.** Once xmorph stops services and `pivot_root`s,
  the old OS is gone. If the installer fails before writing a working
  bootloader, the machine will not boot.
- **`/mnt/oldroot` is ephemeral by design here.** The installer is about to
  repartition the disk that holds the old root. Anything you wanted from
  the old OS must be copied off before you run xmorph.
- **Test against a VM first.** Every recipe in this directory should be
  exercised against a throwaway VM with the same disk layout as your target
  before you run it on hardware you care about.
- **Have an out-of-band recovery path.** Serial console, IPMI/iDRAC/iLO,
  or physical access to boot a rescue USB. If the installer fails, you
  will need it.
- **Network is required.** Every installer downloads the actual OS payload
  from the project's release server at runtime. None of these recipes work
  offline.

## Shared prerequisites

- xmorph binary installed on the source OS (see the top-level
  [README](../../README.md))
- Root privileges
- Outbound HTTPS to the relevant registries and release servers
- At least ~2 GB free RAM for the installer rootfs (more for NixOS)
- A target disk large enough for the chosen OS (see each page)

## Common pattern

All five recipes follow the same shape:

1. A `Containerfile` that bundles your install-time config into a public
   upstream image.
2. An entrypoint script that installs any extra tools the upstream image
   lacks, runs the installer, and reboots.
3. A `xmorph pivot --containerfile Containerfile` invocation.

The entrypoint script always ends with:

```sh
sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

`reboot -f` works in most installer images; the `sysrq-trigger` fallback
covers minimal images that don't ship a `reboot` binary. After this the
firmware reloads from the freshly written disk into the new OS.

A note on the Containerfiles below: xmorph's Containerfile parser does
not currently support `RUN`, so any package installation has to happen in
the entrypoint script at runtime (after pivot), not at image-build time.
The recipes are written with this in mind.

## Recovery

If an install fails:

- The machine will likely fail to boot, or boot into a half-written disk.
- Recover via the out-of-band path you set up before running xmorph.
- Boot a rescue USB / installer ISO / IPMI virtual media.
- From there, you can either reinstall from scratch or restore from the
  backup you took before running xmorph (you did take one).

xmorph itself has no rollback — the old OS was overwritten the moment
the installer's partitioning step ran.
