#!/usr/bin/env bash
set -euo pipefail
usb_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
exec bash "$usb_root/apps/buding-box/launcher/unix-start.sh"
