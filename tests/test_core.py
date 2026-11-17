import subprocess
from bluetooth_recovery.core import BluetoothCtl, Device, Policy, RecoveryController


class FakeBluetooth:
    def __init__(self, connected=False, connects=True):
        self.connected = connected
        self.connects = connects
        self.calls = []

    def is_connected(self, address):
        self.calls.append(("info", address))
        return self.connected

    def connect(self, address):
        self.calls.append(("connect", address))
        return self.connects


def test_recovers_only_registered_disconnected_devices():
    bt = FakeBluetooth()
    recovery = RecoveryController([Device("keyboard", "AA:BB:CC:DD:EE:FF")], Policy(errors_before_recovery=2), bt)
    assert recovery.observe("Bluetooth: hci0: start background scanning failed: -16", now=1) == []
    assert recovery.observe("Bluetooth: hci0: start background scanning failed: -16", now=2) == ["connected:keyboard"]
    assert bt.calls == [("info", "AA:BB:CC:DD:EE:FF"), ("connect", "AA:BB:CC:DD:EE:FF")]


def test_plain_disconnect_is_ignored():
    bt = FakeBluetooth()
    recovery = RecoveryController([Device("keyboard", "AA:BB:CC:DD:EE:FF")], Policy(), bt)
    assert recovery.observe("device disconnected", now=1) == []
    assert bt.calls == []


def test_accepts_a_configured_additional_trigger_pattern():
    bt = FakeBluetooth()
    policy = Policy(errors_before_recovery=1, trigger_patterns=("adapter timeout",))
    recovery = RecoveryController([Device("mouse", "11:22:33:44:55:66")], policy, bt)
    assert recovery.observe("Bluetooth: hci0: adapter timeout", now=1) == ["connected:mouse"]


def test_adapter_reset_is_opt_in(tmp_path):
    reset = tmp_path / "reset"
    reset.write_text("")
    bt = FakeBluetooth(connects=False)
    policy = Policy(errors_before_recovery=1, allow_adapter_reset=True, adapter_reset_path=reset)
    recovery = RecoveryController([Device("keyboard", "AA:BB:CC:DD:EE:FF")], policy, bt)
    assert recovery.observe("Bluetooth: hci0: start background scanning failed: -16", now=1) == ["connect-failed:keyboard", "adapter-reset"]
    assert reset.read_text() == "1\n"


def test_paired_parser_handles_bluetoothctl_output():
    def run(*args, **kwargs):
        return subprocess.CompletedProcess([], 0, "Device AA:BB:CC:DD:EE:FF BT5.0 KB\n", "")
    assert BluetoothCtl(run).paired() == {"BT5.0 KB": "AA:BB:CC:DD:EE:FF"}
