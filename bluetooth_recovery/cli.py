from __future__ import annotations

import argparse
import subprocess
import sys
import tomllib
from pathlib import Path

from .core import DEFAULT_TRIGGER_PATTERN, BluetoothCtl, Device, Policy, RecoveryController


DEFAULT_CONFIG = Path("/etc/bluetooth-device-recovery/config.toml")


def load_config(path: Path) -> tuple[list[Device], Policy]:
    data = tomllib.loads(path.read_text())
    policy_data = data.get("policy", {})
    policy = Policy(
        errors_before_recovery=int(policy_data.get("errors_before_recovery", 3)),
        error_window_seconds=int(policy_data.get("error_window_seconds", 45)),
        cooldown_seconds=int(policy_data.get("cooldown_seconds", 120)),
        allow_adapter_reset=bool(policy_data.get("allow_adapter_reset", False)),
        adapter_reset_path=Path(policy_data.get("adapter_reset_path", "/sys/class/bluetooth/hci0/reset")),
        trigger_patterns=tuple(policy_data.get("trigger_patterns", [DEFAULT_TRIGGER_PATTERN])),
    )
    devices = [
        Device(name=item["name"], address=item["address"].upper(), enabled=bool(item.get("enabled", True)))
        for item in data.get("device", [])
    ]
    if not devices:
        raise ValueError("at least one [[device]] entry is required")
    return devices, policy


def discover() -> int:
    for name, address in BluetoothCtl().paired().items():
        print(f'[[device]]\nname = "{name}"\naddress = "{address}"\nenabled = true\n')
    return 0


def watch(config: Path) -> int:
    devices, policy = load_config(config)
    controller = RecoveryController(devices, policy, BluetoothCtl())
    command = ["journalctl", "-k", "-f", "-n", "0", "-o", "cat"]
    with subprocess.Popen(command, text=True, stdout=subprocess.PIPE) as journal:
        assert journal.stdout is not None
        for line in journal.stdout:
            for action in controller.observe(line):
                print(action, flush=True)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Recover selected Bluetooth devices only")
    parser.add_argument("--config", type=Path, default=DEFAULT_CONFIG)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("discover", help="print paired devices as TOML entries")
    subparsers.add_parser("watch", help="watch kernel Bluetooth failures and recover registered devices")
    args = parser.parse_args()
    try:
        return discover() if args.command == "discover" else watch(args.config)
    except (OSError, ValueError, tomllib.TOMLDecodeError) as error:
        print(f"bt-device-recovery: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
