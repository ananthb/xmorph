# Any Linux → Flatcar Container Linux

Replaces the running OS with [Flatcar Container Linux](https://www.flatcar.org/)
using the upstream `flatcar-install` script wrapped in an Alpine container.

## Trusted source

| Artifact | Source |
|---|---|
| Wrapper image base | `docker.io/alpine:latest` (Docker Official, maintained by Alpine's founder) |
| `flatcar-install` script | `https://raw.githubusercontent.com/flatcar/init/flatcar-master/bin/flatcar-install` (upstream, fetched at runtime) |
| Flatcar disk image | `https://<channel>.release.flatcar-linux.net/<board>/<version>/` (GPG-verified by `flatcar-install` against the embedded Flatcar Buildbot key) |
| Config | Your own Ignition config, layered in via `--rootfs` |

Flatcar does not publish an official installer container, so we use the
upstream shell script. Image fetching is GPG-verified; the script itself
should be pinned to a commit (see below).

## Prerequisites

- Outbound HTTPS to `raw.githubusercontent.com` (script) and
  `*.release.flatcar-linux.net` (Flatcar images)
- Target disk at least 8 GiB; cannot be the disk the running OS currently
  uses as its root mount (fine — xmorph has pivoted to RAM)
- BIOS or UEFI; the Flatcar disk image is hybrid GPT and boots both
- 1 GiB+ RAM

## Files

### `config.bu` — Butane source for your Ignition config

```yaml
variant: flatcar
version: 1.0.0
passwd:
  users:
    - name: core
      ssh_authorized_keys:
        - ssh-ed25519 AAAA... your-key-here
storage:
  files:
    - path: /etc/hostname
      mode: 0644
      contents:
        inline: myhost
```

Transpile to Ignition JSON:

```sh
podman run --rm -i quay.io/coreos/butane:release < config.bu > config.ign
```

### `install.sh` — entrypoint

Pin `FLATCAR_INSTALL_REF` to a specific commit SHA from
[`flatcar/init`](https://github.com/flatcar/init/commits/flatcar-master)
so you're not silently picking up upstream changes between runs.

```sh
#!/bin/sh
set -eu

FLATCAR_INSTALL_REF="${FLATCAR_INSTALL_REF:-flatcar-master}"
FLATCAR_CHANNEL="${FLATCAR_CHANNEL:-stable}"
FLATCAR_VERSION="${FLATCAR_VERSION:-current}"

apk add --no-cache \
  bash gnupg wget lbzip2 util-linux e2fsprogs btrfs-progs \
  lvm2 gawk efibootmgr ca-certificates coreutils

wget -O /usr/local/bin/flatcar-install \
  "https://raw.githubusercontent.com/flatcar/init/${FLATCAR_INSTALL_REF}/bin/flatcar-install"
chmod +x /usr/local/bin/flatcar-install

# -V current means "latest in channel"; pin with -V <version> for reproducibility
flatcar-install -d /dev/sda \
  -C "${FLATCAR_CHANNEL}" \
  -V "${FLATCAR_VERSION}" \
  -i /etc/config.ign

sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

### Overlay layout

Put your install script and Ignition config into a directory tree that
mirrors the paths you want in the pivoted rootfs:

```
overlay/
├── install.sh          # chmod +x
└── etc/
    └── config.ign
```

## Run it

```sh
sudo xmorph pivot \
  --image docker.io/alpine:latest \
  --rootfs ./overlay/ \
  --entrypoint /install.sh \
  --force
```

To pin the Flatcar version explicitly:

```sh
# Edit install.sh to set:
#   FLATCAR_VERSION=4593.2.3
# then run xmorph as above.
```

## Reprovisioning a remote host

The `--force` invocation above assumes you can watch the console. On a
headless box reached only over SSH, the pivot tears down the old OS's
networking the moment services stop — the session that launched xmorph
goes with it, and you're blind while `flatcar-install` rewrites the disk.
Bring your own reachability into the in-RAM installer rootfs:

```sh
sudo xmorph pivot \
  --image docker.io/alpine:latest \
  --rootfs ./overlay/ \
  --entrypoint /install.sh \
  --tailscale.authkey tskey-auth-xxxxx \
  --headless \
  --watchdog-timeout 20m \
  --force
```

- **`--tailscale.authkey`** joins the pivoted rootfs to your tailnet via
  tsnet (userspace, so it survives losing the host's own networking).
  Default `tailscale up` args are `--ssh --hostname=<host>-xmorph`, so the
  host reappears as `<host>-xmorph` with Tailscale SSH while the installer
  runs. Use `--tailscale.server` for a Headscale coordination server.
  (Plain `--ssh.enable --ssh.authorized-keys` also starts an sshd in the
  installer, but only helps if the rootfs still holds a routable address
  after pivot — Tailscale doesn't depend on that.)
- **`--headless`** detaches xmorph from the launching terminal (implies
  `--force`), so an SSH disconnect doesn't kill the install mid-write.
- **`--watchdog-timeout`** resets the box if the entrypoint hangs, turning
  a wedged install into a reboot instead of a silent brick. Still keep an
  out-of-band recovery path — `flatcar-install` failing before it writes a
  working bootloader leaves the disk unbootable.

## What happens

1. xmorph pulls `alpine:latest`, merges `./overlay/` on top, and pivots
   into the combined rootfs.
2. `install.sh` `apk add`s the tools `flatcar-install` needs.
3. It fetches `flatcar-install` from the pinned ref on GitHub.
4. `flatcar-install`:
   - Downloads the Flatcar production image (`.bin.bz2`) and `.sig` from
     `release.flatcar-linux.net`
   - GPG-verifies the signature against the embedded Flatcar Buildbot
     public key
   - `dd`s the image to `/dev/sda` (prebuilt hybrid-GPT disk image; no
     manual partitioning needed)
   - Mounts the OEM partition and copies your `config.ign` to
     `/oemfs/config.ign`
5. The script syncs and reboots.
6. Firmware loads the disk; Flatcar boots, Ignition runs on first boot.

## Verify

```sh
ssh core@<host>
cat /etc/os-release   # NAME="Flatcar Container Linux by Kinvolk"
update_engine_client -status   # update agent should be running
```

## Notes and gotchas

- **`-V current` vs pinned version**: `current` resolves to the channel's
  latest at runtime — convenient, not reproducible. For fleet rollouts,
  pin to a specific version.
- **No URL-based Ignition**: `flatcar-install -i` only accepts a local
  file path. To fetch a config from a URL, either download it in
  `install.sh` first, or use a small Ignition that references a remote
  config via `ignition.config.replace`.
- **UEFI NVRAM**: pass `-u` to `flatcar-install` to add an `efibootmgr`
  NVRAM entry. Useful on hardware that doesn't auto-detect the ESP.
- **Mirror override**: `flatcar-install -b <url>` lets you point at an
  internal mirror of `release.flatcar-linux.net` for air-gapped or
  bandwidth-sensitive installs.
- **Custom GPG key**: `-k <keyfile>` lets you verify against a different
  key (e.g. for internal builds).

## Source docs

- [Flatcar bare-metal installing-to-disk](https://www.flatcar.org/docs/latest/installing/bare-metal/installing-to-disk/)
- [`flatcar-install` source](https://github.com/flatcar/init/blob/flatcar-master/bin/flatcar-install)
- [Butane config spec](https://coreos.github.io/butane/specs/)
