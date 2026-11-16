#!/usr/bin/env bash
# Prints the runner kernel, cgroup and container facts that decide which Chaos Mesh
# fault types can work, as a Markdown table.
set -uo pipefail
kernel="$(uname -r)"
config="/boot/config-$kernel"
kconfig() {
  test -r "$config" || return 0
  grep -E "^$1=" "$config" | cut -d= -f2 | grep . || echo 'not set'
}
row() { printf '| %s | %s |\n' "$1" "${2:-unknown}"; }

echo "### Runner facts ($(uname -m))"
echo
echo '| Fact | Value |'
echo '| --- | --- |'
row OS "$(. /etc/os-release && echo "$PRETTY_NAME")"
row Kernel "$kernel"
row 'CPUs / memory' "$(nproc) / $(free -g | awk '/Mem:/ {print $2}') GiB"
row 'cgroup filesystem' "$(stat -fc %T /sys/fs/cgroup)"
row Docker "$(docker info --format '{{.ServerVersion}}, cgroup {{.CgroupVersion}} ({{.CgroupDriver}}), {{.Driver}}' 2>/dev/null)"
row 'Passwordless sudo' "$(sudo -n true 2>/dev/null && echo yes || echo no)"
row '/dev/fuse' "$(test -c /dev/fuse && echo present || echo absent)"
row 'Kernel headers' "$(test -d "/lib/modules/$kernel/build" && echo installed || (apt-cache show "linux-headers-$kernel" >/dev/null 2>&1 && echo 'available via apt' || echo unavailable))"
row Lockdown "$(cat /sys/kernel/security/lockdown 2>/dev/null)"
row 'Module signature enforcement' "$(cat /sys/module/module/parameters/sig_enforce 2>/dev/null)"
row 'Secure Boot' "$(mokutil --sb-state 2>/dev/null | head -1)"
for option in CONFIG_FUSE_FS CONFIG_BPF_SYSCALL CONFIG_KPROBES CONFIG_FUNCTION_ERROR_INJECTION \
  CONFIG_BPF_KPROBE_OVERRIDE CONFIG_FAIL_MAKE_REQUEST CONFIG_MODULE_SIG_FORCE; do
  row "$option" "$(kconfig "$option")"
done
