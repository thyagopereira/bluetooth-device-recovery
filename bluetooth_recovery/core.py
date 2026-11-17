from __future__ import annotations

import re
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Iterable


DEFAULT_TRIGGER_PATTERN = "start background scanning failed: -16"
ADDRESS = re.compile(r"^Device ([0-9A-F:]{17}) (.+)$", re.MULTILINE)


@dataclass(frozen=True)
class Device:
    name: str
    address: str
    enabled: bool = True


@dataclass(frozen=True)
class Policy:
    errors_before_recovery: int = 3
    error_window_seconds: int = 45
    cooldown_seconds: int = 120
    allow_adapter_reset: bool = False
    adapter_reset_path: Path = Path("/sys/class/bluetooth/hci0/reset")
    trigger_patterns: tuple[str, ...] = (DEFAULT_TRIGGER_PATTERN,)


class BluetoothCtl:
    def __init__(self, run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run):
        self.run = run

    def command(self, *args: str) -> subprocess.CompletedProcess[str]:
        return self.run(
            ["bluetoothctl", *args], text=True, capture_output=True, check=False
        )

    def paired(self) -> dict[str, str]:
        result = self.command("devices", "Paired")
        return {match.group(2): match.group(1) for match in ADDRESS.finditer(result.stdout)}

    def is_connected(self, address: str) -> bool:
        result = self.command("info", address)
        return "Connected: yes" in result.stdout

    def connect(self, address: str) -> bool:
        result = self.command("connect", address)
        return result.returncode == 0 and "successful" in result.stdout.lower()


class RecoveryController:
    def __init__(self, devices: Iterable[Device], policy: Policy, bluetooth: BluetoothCtl):
        self.devices = tuple(device for device in devices if device.enabled)
        self.policy = policy
        self.bluetooth = bluetooth
        self.errors: list[float] = []
        self.last_recovery: float | None = None

    def observe(self, line: str, now: float | None = None) -> list[str]:
        """Consume one journal line and return recovery actions taken.

        No action is taken for ordinary device disconnections. This prevents a
        device intentionally switched to another host from being pulled back.
        """
        if not any(pattern in line for pattern in self.policy.trigger_patterns):
            return []
        now = time.monotonic() if now is None else now
        self.errors = [stamp for stamp in self.errors if now - stamp <= self.policy.error_window_seconds]
        self.errors.append(now)
        if len(self.errors) < self.policy.errors_before_recovery:
            return []
        if self.last_recovery is not None and now - self.last_recovery < self.policy.cooldown_seconds:
            return []
        self.last_recovery = now
        self.errors.clear()
        return self.recover()

    def recover(self) -> list[str]:
        actions: list[str] = []
        for device in self.devices:
            if self.bluetooth.is_connected(device.address):
                continue
            if self.bluetooth.connect(device.address):
                actions.append(f"connected:{device.name}")
            else:
                actions.append(f"connect-failed:{device.name}")
        if self.policy.allow_adapter_reset and any(action.startswith("connect-failed:") for action in actions):
            self.policy.adapter_reset_path.write_text("1\n")
            actions.append("adapter-reset")
        return actions
