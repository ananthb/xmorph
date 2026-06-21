# Any Linux → Alpine Linux

Replaces the running OS with [Alpine Linux](https://alpinelinux.org/)
using `setup-disk` from the official Alpine container.

## Trusted source

| Artifact | Source |
|---|---|
| Installer image | `docker.io/alpine:latest` (Docker Official, maintained by Alpine's founder; built at `github.com/alpinelinux/docker-alpine`) |
| `alpine-conf` (`setup-disk`, `setup-alpine`) | Installed via `apk` from `dl-cdn.alpinelinux.org` at runtime |
| Alpine APK packages | `https://dl-cdn.alpinelinux.org/alpine/` |
| Config | Your answers, baked into the Containerfile as env exports |

The base Alpine image is ~5 MB and ships only musl + BusyBox + apk; the
installer tools are added at runtime.

## Prerequisites

- Outbound HTTPS to `dl-cdn.alpinelinux.org`
- Target disk ≥ 1 GiB (700 MB is the absolute floor for `sys` mode)
- 256 MiB+ RAM (more realistically 512 MiB for headroom)
- BIOS (default) or UEFI (set `USE_EFI=1`)

## Files

### `install.sh` — entrypoint

```sh
#!/bin/sh
set -eu

DISK="${DISK:-/dev/sda}"
HOSTNAME="${HOSTNAME:-myhost}"
ALPINE_BRANCH="${ALPINE_BRANCH:-v3.20}"
USE_EFI="${USE_EFI:-0}"

# Use a current mirror as the apk source for the install image itself.
cat > /etc/apk/repositories <<EOF
https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/main
https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/community
EOF
apk update
apk add --no-cache alpine-conf e2fsprogs dosfstools util-linux openssh

if [ "$USE_EFI" = "1" ]; then
  apk add --no-cache grub-efi efibootmgr
  export USE_EFI=1
  export BOOTLOADER=grub
else
  apk add --no-cache syslinux
  export BOOTLOADER=syslinux
fi

# Seed identity + network for setup-disk's "transfer running config to disk" step.
setup-hostname "$HOSTNAME"
cat > /etc/network/interfaces <<EOF
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp
EOF

# Install your SSH key for root on the new disk.
mkdir -p /root/.ssh
chmod 700 /root/.ssh
cp /etc/authorized_keys /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys

# Allow root SSH with key auth on the installed system.
rc-update add sshd default 2>/dev/null || true
mkdir -p /etc/ssh
cat > /etc/ssh/sshd_config.d/10-installed.conf <<'EOF'
PasswordAuthentication no
PermitRootLogin prohibit-password
EOF

# Non-interactive sys-mode install. ERASE_DISKS must be set or setup-disk prompts.
ERASE_DISKS="$DISK" \
DISKLABEL=gpt \
setup-disk -m sys "$DISK"

sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

`setup-disk -m sys` is the "traditional disk install" mode: separate
`/boot`, `/`, and swap; the system runs from disk. The other modes
(`data`, `lvm`) are for specialty setups.

### `authorized_keys` — public key(s) for `root` on the new system

```
ssh-ed25519 AAAA... your-key-here
```

### `Containerfile`

```dockerfile
FROM docker.io/alpine:latest
COPY install.sh /install.sh
COPY authorized_keys /etc/authorized_keys
ENTRYPOINT ["/install.sh"]
```

## Run it

```sh
sudo xmorph pivot --containerfile ./Containerfile --force
```

For UEFI:

```sh
# Edit install.sh to set USE_EFI=1, or pass via env in a wrapper.
```

## What happens

1. xmorph builds the OCI image and pivots into it.
2. `install.sh`:
   - Points apk at the chosen Alpine branch mirror
   - `apk add`s `alpine-conf` (which provides `setup-*`) and filesystem
     + bootloader tooling
   - Seeds hostname, network interfaces, and SSH authorized_keys into
     the *running* environment — `setup-disk` copies the running config
     onto the new root
   - Calls `ERASE_DISKS=… setup-disk -m sys /dev/sda`, which:
     - Partitions the disk (GPT or DOS per `DISKLABEL`)
     - Creates `/boot`, swap, `/`
     - Formats and mounts under `/mnt`
     - Installs the base packages plus the running system's config
     - Installs the bootloader (syslinux for BIOS, grub-efi for UEFI)
3. The script syncs and reboots.
4. Firmware loads the bootloader; Alpine boots.

## Verify

```sh
ssh root@<host>
cat /etc/alpine-release   # 3.20.x
rc-status                 # OpenRC service status
```

## Notes and gotchas

- **`ERASE_DISKS` is mandatory for unattended runs** — without it,
  `setup-disk` will prompt "Erase the above disk(s)? (y/n)" and hang
  forever in xmorph's pivoted environment (no TTY).
- **The "config is copied from the running env"** model means anything
  you want on the new system (hostname, `/etc/network/interfaces`,
  `/root/.ssh/authorized_keys`, installed packages) needs to be present
  in the installer container before `setup-disk` runs. The script above
  does this explicitly; `setup-alpine -f answers` is the alternative if
  you want the documented Alpine answerfile format.
- **UEFI NVRAM**: `setup-disk` writes to `/EFI/alpine/` and `/EFI/boot/`
  but does *not* create an NVRAM boot entry. If your firmware doesn't
  auto-fall-back to `\EFI\boot\bootx64.efi`, add an entry yourself:
  `efibootmgr -c -d /dev/sda -p 1 -L Alpine -l '\EFI\alpine\grubx64.efi'`
  before rebooting.
- **Branch pinning**: `ALPINE_BRANCH=v3.20` pins the installed system to
  that release. `edge` pulls rolling Alpine — almost certainly not what
  you want for a permanent install.
- **Package set**: `setup-disk` installs only the base. Add anything else
  (Tailscale, your services, etc.) by either `chroot /mnt apk add …`
  after `setup-disk` and before the reboot, or via an
  `/etc/local.d/*.start` script that runs on first boot.

## Source docs

- [`alpine-conf` source (setup-disk.in)](https://git.alpinelinux.org/alpine-conf/tree/setup-disk.in)
- [Alpine wiki: Using an answerfile with setup-alpine](https://wiki.alpinelinux.org/wiki/Using_an_answerfile_with_setup-alpine)
- [Alpine wiki: Bootloaders](https://wiki.alpinelinux.org/wiki/Bootloaders)
- [Alpine wiki: UEFI](https://wiki.alpinelinux.org/wiki/UEFI)
- [docker-alpine repo](https://github.com/alpinelinux/docker-alpine)
