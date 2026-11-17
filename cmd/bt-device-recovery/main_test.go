package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeBluetooth struct {
	connected, connects bool
	calls               []string
}

func (f *fakeBluetooth) Connected(address string) bool {
	f.calls = append(f.calls, "info:"+address)
	return f.connected
}
func (f *fakeBluetooth) Connect(address string) bool {
	f.calls = append(f.calls, "connect:"+address)
	return f.connects
}

func testConfig() Config {
	return Config{Policy: Policy{ErrorsBeforeRecovery: 2, ErrorWindowSeconds: 45, CooldownSeconds: 120, TriggerPatterns: []string{"adapter busy"}}, Devices: []Device{{Name: "keyboard", Address: "AA:BB:CC:DD:EE:FF", Enabled: true}}}
}

func TestRecoversOnlyRegisteredDevice(t *testing.T) {
	fake := &fakeBluetooth{connects: true}
	controller := &Controller{config: testConfig(), bluetooth: fake}
	now := time.Now()
	if got := controller.Observe("adapter busy", now); len(got) != 0 {
		t.Fatalf("first error action = %v", got)
	}
	got := controller.Observe("adapter busy", now.Add(time.Second))
	if len(got) != 1 || got[0] != "connected:keyboard" {
		t.Fatalf("actions = %v", got)
	}
	if len(fake.calls) != 2 || fake.calls[1] != "connect:AA:BB:CC:DD:EE:FF" {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestNormalDisconnectDoesNothing(t *testing.T) {
	fake := &fakeBluetooth{}
	controller := &Controller{config: testConfig(), bluetooth: fake}
	if got := controller.Observe("device disconnected", time.Now()); len(got) != 0 || len(fake.calls) != 0 {
		t.Fatalf("unexpected action: %v / %v", got, fake.calls)
	}
}

func TestResetIsOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.Policy.ErrorsBeforeRecovery = 1
	config.Policy.AllowAdapterReset = true
	config.Policy.AdapterResetPath = path
	controller := &Controller{config: config, bluetooth: &fakeBluetooth{connects: false}}
	got := controller.Observe("adapter busy", time.Now())
	if len(got) != 2 || got[1] != "adapter-reset" {
		t.Fatalf("actions = %v", got)
	}
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != "1\n" {
		t.Fatalf("reset = %q, %v", bytes, err)
	}
}
