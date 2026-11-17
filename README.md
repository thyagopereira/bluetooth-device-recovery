# Bluetooth Device Recovery

Linux watchdog for recovering **only registered Bluetooth devices** when a known adapter failure occurs. It is designed for keyboards and mice that are deliberately switched between hosts: a normal disconnect is ignored, so the watchdog never pulls a device back just because you selected another pairing slot.

## What it does

1. Watches the kernel journal for the MT7925 failure seen on this machine: `start background scanning failed: -16`.
2. Requires several failures inside a configured time window.
3. Attempts `bluetoothctl connect` for each registered, disconnected device.
4. Leaves every unregistered device untouched.

The optional adapter reset is disabled by default. A Bluetooth controller cannot reset one device in isolation, so enabling it can briefly disconnect all Bluetooth peripherals. It is only attempted after a targeted reconnect fails.

## Register a device

While the device works, run:

```bash
bt-device-recovery discover
```

Copy the relevant generated `[[device]]` block into `/etc/bluetooth-device-recovery/config.toml`. Add another block for each device you want the service to manage, such as a Logitech M720 Triathlon. Never use a display name alone: the Bluetooth address makes the target unambiguous.

The M720 must be connected by **Bluetooth** to be managed here. If it is using its Logitech USB receiver, it is not a Bluetooth device and needs a separate USB-receiver watchdog instead.

`trigger_patterns` is also configurable. Add a new pattern only after collecting a log line that reliably identifies the adapter failure; ordinary disconnects must never be triggers.

## Install

```bash
sudo install -d -m 755 /opt/bluetooth-device-recovery
sudo cp -r bluetooth_recovery main.py /opt/bluetooth-device-recovery/
sudo install -Dm644 config.example.toml /etc/bluetooth-device-recovery/config.toml
sudoedit /etc/bluetooth-device-recovery/config.toml
sudo install -Dm644 systemd/bluetooth-device-recovery.service /etc/systemd/system/bluetooth-device-recovery.service
sudo systemctl daemon-reload
sudo systemctl enable --now bluetooth-device-recovery.service
```

Then check it with:

```bash
systemctl status bluetooth-device-recovery.service
journalctl -u bluetooth-device-recovery.service -f
```

## Test

```bash
python3 -m pytest -q
```

## Safety model

- A normal disconnect is never a trigger.
- The error signature has a threshold and a cooldown.
- Direct connection attempts are limited to entries under `[[device]]`.
- Adapter reset requires an explicit `allow_adapter_reset = true` opt-in.
