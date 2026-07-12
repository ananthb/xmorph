# Any Linux → Ubuntu 24.04 (noble)

Replaces the running OS with Ubuntu 24.04 LTS using
[`debootstrap`](https://manpages.ubuntu.com/manpages/noble/man8/debootstrap.8.html)
from the official Debian container. Post-install config is handled by
cloud-init via the NoCloud seed directory.

## Trusted source

| Artifact | Source |
|---|---|
| Installer image | `docker.io/debian:stable` (Debian Official) |
| `debootstrap` | Installed via `apt-get` from Debian's repos at runtime |
| Ubuntu rootfs | `http://archive.ubuntu.com/ubuntu/` (Ubuntu's official archive) |
| Kernel, grub, cloud-init | Installed via Ubuntu's apt mirror during chroot phase |
| Config | Your cloud-init `user-data`, layered in via `--rootfs` |

Why `debian:stable` and not `ubuntu:noble`? Either works, but Debian's
`debootstrap` package consistently carries up-to-date scripts for both
Debian and Ubuntu suites. If you'd rather not cross distros, swap the
`FROM` line below.

## Prerequisites

- Outbound HTTPS to `deb.debian.org` (for debootstrap) and
  `archive.ubuntu.com` + `security.ubuntu.com` (for the Ubuntu rootfs
  and apt packages)
- Target disk ≥ 8 GiB (5 GiB minimum; 25 GiB suggested by Ubuntu)
- 1.5 GiB+ RAM
- BIOS or UEFI — the partitioning differs; both shown below

## Files

### `user-data` — cloud-init NoCloud seed

```yaml
#cloud-config
hostname: myhost
manage_etc_hosts: true
users:
  - name: ubuntu
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - ssh-ed25519 AAAA... your-key-here
ssh_pwauth: false
package_update: true
```

### `install.sh` — entrypoint (UEFI variant)

```sh
#!/bin/sh
set -eu

DISK="${DISK:-/dev/sda}"
SUITE="${SUITE:-noble}"
MIRROR="${MIRROR:-http://archive.ubuntu.com/ubuntu/}"

apt-get update
apt-get install -y --no-install-recommends \
  debootstrap parted gdisk dosfstools e2fsprogs uuid-runtime \
  ca-certificates wget

# Partition: 512M ESP + ext4 root (UEFI)
sgdisk -Z "$DISK"
sgdisk -n1:0:+512M -t1:EF00 -c1:ESP "$DISK"
sgdisk -n2:0:0     -t2:8300 -c2:root "$DISK"
partprobe "$DISK"

mkfs.vfat -F32 -n ESP  "${DISK}1"
mkfs.ext4 -F  -L root  "${DISK}2"

mount "${DISK}2" /mnt
mkdir -p /mnt/boot/efi
mount "${DISK}1" /mnt/boot/efi

debootstrap --arch=amd64 --variant=minbase "$SUITE" /mnt "$MIRROR"

# fstab
ROOT_UUID=$(blkid -s UUID -o value "${DISK}2")
EFI_UUID=$(blkid -s UUID -o value "${DISK}1")
cat > /mnt/etc/fstab <<EOF
UUID=$ROOT_UUID  /          ext4  defaults              0 1
UUID=$EFI_UUID   /boot/efi  vfat  umask=0077,fmask=0077 0 1
EOF

# Apt sources for noble main universe
cat > /mnt/etc/apt/sources.list <<EOF
deb $MIRROR $SUITE main universe
deb $MIRROR $SUITE-updates main universe
deb http://security.ubuntu.com/ubuntu/ $SUITE-security main universe
EOF

# Hostname + minimal hosts
echo "myhost" > /mnt/etc/hostname
cat > /mnt/etc/hosts <<EOF
127.0.0.1   localhost
127.0.1.1   myhost
EOF

# Cloud-init NoCloud seed (consumed on first boot)
mkdir -p /mnt/var/lib/cloud/seed/nocloud
cp /etc/user-data /mnt/var/lib/cloud/seed/nocloud/user-data
cat > /mnt/var/lib/cloud/seed/nocloud/meta-data <<EOF
instance-id: $(uuidgen)
local-hostname: myhost
EOF

# Bind-mount kernel filesystems for chroot
for d in dev dev/pts proc sys run; do
  mkdir -p "/mnt/$d"
  mount --rbind "/$d" "/mnt/$d"
  mount --make-rslave "/mnt/$d"
done

# Install kernel, bootloader, cloud-init inside the new root
chroot /mnt /bin/bash -eu <<'CHROOT'
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  linux-image-generic \
  grub-efi-amd64 \
  efibootmgr \
  cloud-init \
  netplan.io \
  openssh-server \
  ca-certificates
grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=ubuntu
update-grub
systemctl enable ssh cloud-init cloud-config cloud-final cloud-init-local
CHROOT

sync
reboot -f 2>/dev/null || echo b > /proc/sysrq-trigger
```

For **BIOS instead of UEFI**, change the partition layout to a small
`EF02` bios_grub partition + ext4 root, and replace the grub block with
`apt-get install -y grub-pc && grub-install --target=i386-pc "$DISK" &&
update-grub`. Skip `efibootmgr`.

### Overlay layout

```
overlay/
├── install.sh          # chmod +x
└── etc/
    └── user-data
```

## Run it

```sh
sudo xmorph pivot \
  --image docker.io/debian:stable \
  --rootfs ./overlay/ \
  --entrypoint /install.sh \
  --force
```

To install to a different disk:

```sh
# Edit install.sh and change DISK=, or set DISK at xmorph entrypoint time
# by wrapping install.sh.
```

## What happens

1. xmorph pulls `debian:stable`, merges `./overlay/` (your `user-data`
   and `install.sh`) on top, and pivots into the combined rootfs.
2. `install.sh`:
   - `apt-get install`s debootstrap + partitioning tools
   - Wipes and partitions `/dev/sda` (GPT: 512M ESP + ext4 root)
   - Runs `debootstrap noble /mnt` from `archive.ubuntu.com`
   - Writes `/etc/fstab`, `/etc/apt/sources.list`, `/etc/hostname`
   - Drops your `user-data` into `/mnt/var/lib/cloud/seed/nocloud/`
   - Chroots into `/mnt` and installs kernel + grub + cloud-init from
     the Ubuntu mirror
   - Runs `grub-install` + `update-grub`
3. The script syncs and reboots.
4. Firmware loads GRUB → Ubuntu kernel; cloud-init runs on first boot
   from the NoCloud seed and applies your `user-data`.

## Verify

```sh
ssh ubuntu@<host>
lsb_release -a   # Ubuntu 24.04 LTS
cloud-init status   # done
```

## Notes and gotchas

- **debootstrap script name resolution**: `debootstrap noble ...` requires
  that the `noble` script is present in `/usr/share/debootstrap/scripts/`.
  If it's missing in `debian:stable` at the time you run, switch the base
  image to `docker.io/ubuntu:noble` (which always knows itself), or
  symlink: `ln -s gutsy /usr/share/debootstrap/scripts/noble`.
- **NoCloud seed quirks**: `/var/lib/cloud/seed/nocloud/` is the
  documented filesystem path the NoCloud datasource probes on disk. The
  `meta-data` file must exist and contain at least `instance-id` — that
  field is how cloud-init decides whether it has already run.
- **mmdebstrap is faster**: if the install time hurts, `mmdebstrap`
  is a near drop-in replacement (`apt-get install mmdebstrap` instead;
  same CLI shape) that parallelizes the bootstrap.
- **curtin is the polished alternative**: Canonical's own installer
  backend (used by subiquity) takes a declarative YAML and handles
  partitioning, bootloader, and network. Worth considering if you outgrow
  the debootstrap recipe.
- **Pin the kernel**: `linux-image-generic` is a meta-package that
  follows updates. For reproducibility, install a specific
  `linux-image-X.Y.Z-NNN-generic`.

## Source docs

- [Ubuntu debootstrap (Community help)](https://help.ubuntu.com/community/DebootstrapChroot)
- [debootstrap manpage](https://manpages.ubuntu.com/manpages/noble/man8/debootstrap.8.html)
- [cloud-init NoCloud datasource](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)
- [mmdebstrap](https://manpages.ubuntu.com/manpages/jammy/man1/mmdebstrap.1.html)
- [curtin](https://github.com/canonical/curtin)
