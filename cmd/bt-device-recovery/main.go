package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultConfigPath = "/etc/bluetooth-device-recovery/config.json"

type Device struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
}

type Policy struct {
	ErrorsBeforeRecovery int      `json:"errors_before_recovery"`
	ErrorWindowSeconds   int      `json:"error_window_seconds"`
	CooldownSeconds      int      `json:"cooldown_seconds"`
	AllowAdapterReset    bool     `json:"allow_adapter_reset"`
	AdapterResetPath     string   `json:"adapter_reset_path"`
	TriggerPatterns      []string `json:"trigger_patterns"`
}

type Config struct {
	Policy  Policy   `json:"policy"`
	Devices []Device `json:"devices"`
}

type Bluetooth interface {
	Connected(address string) bool
	Connect(address string) bool
}

type BluetoothCtl struct{}

func (BluetoothCtl) output(args ...string) (string, error) {
	out, err := exec.Command("bluetoothctl", args...).CombinedOutput()
	return string(out), err
}

func (b BluetoothCtl) Connected(address string) bool {
	out, _ := b.output("info", address)
	return strings.Contains(out, "Connected: yes")
}

func (b BluetoothCtl) Connect(address string) bool {
	out, err := b.output("connect", address)
	return err == nil && strings.Contains(strings.ToLower(out), "successful")
}

type Controller struct {
	config       Config
	bluetooth    Bluetooth
	errors       []time.Time
	lastRecovery time.Time
}

func (c *Controller) Observe(line string, now time.Time) []string {
	matched := false
	for _, pattern := range c.config.Policy.TriggerPatterns {
		if strings.Contains(line, pattern) {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	window := time.Duration(c.config.Policy.ErrorWindowSeconds) * time.Second
	kept := c.errors[:0]
	for _, stamp := range c.errors {
		if now.Sub(stamp) <= window {
			kept = append(kept, stamp)
		}
	}
	c.errors = append(kept, now)
	if len(c.errors) < c.config.Policy.ErrorsBeforeRecovery || (!c.lastRecovery.IsZero() && now.Sub(c.lastRecovery) < time.Duration(c.config.Policy.CooldownSeconds)*time.Second) {
		return nil
	}
	c.errors = nil
	c.lastRecovery = now
	return c.Recover()
}

func (c *Controller) Recover() []string {
	actions := []string{}
	failed := false
	for _, device := range c.config.Devices {
		if !device.Enabled || c.bluetooth.Connected(device.Address) {
			continue
		}
		if c.bluetooth.Connect(device.Address) {
			actions = append(actions, "connected:"+device.Name)
		} else {
			actions = append(actions, "connect-failed:"+device.Name)
			failed = true
		}
	}
	if failed && c.config.Policy.AllowAdapterReset {
		if err := os.WriteFile(c.config.Policy.AdapterResetPath, []byte("1\n"), 0); err == nil {
			actions = append(actions, "adapter-reset")
		} else {
			actions = append(actions, "adapter-reset-failed:"+err.Error())
		}
	}
	return actions
}

func loadConfig(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(bytes, &config); err != nil {
		return Config{}, err
	}
	if len(config.Devices) == 0 {
		return Config{}, errors.New("at least one device is required")
	}
	for _, device := range config.Devices {
		if device.Name == "" || device.Address == "" {
			return Config{}, errors.New("every device needs name and address")
		}
	}
	if config.Policy.ErrorsBeforeRecovery < 1 {
		config.Policy.ErrorsBeforeRecovery = 3
	}
	if config.Policy.ErrorWindowSeconds < 1 {
		config.Policy.ErrorWindowSeconds = 45
	}
	if config.Policy.CooldownSeconds < 1 {
		config.Policy.CooldownSeconds = 120
	}
	if len(config.Policy.TriggerPatterns) == 0 {
		return Config{}, errors.New("at least one trigger pattern is required")
	}
	if config.Policy.AdapterResetPath == "" {
		config.Policy.AdapterResetPath = "/sys/class/bluetooth/hci0/reset"
	}
	return config, nil
}

func discover() error {
	out, err := exec.Command("bluetoothctl", "devices", "Paired").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bluetoothctl: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "Device" {
			fmt.Printf("{\"name\":%q,\"address\":%q,\"enabled\":true}\n", strings.Join(fields[2:], " "), fields[1])
		}
	}
	return nil
}

func watch(path string) error {
	config, err := loadConfig(path)
	if err != nil {
		return err
	}
	controller := &Controller{config: config, bluetooth: BluetoothCtl{}}
	journal := exec.Command("journalctl", "-k", "-f", "-n", "0", "-o", "cat")
	stdout, err := journal.StdoutPipe()
	if err != nil {
		return err
	}
	if err := journal.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		for _, action := range controller.Observe(scanner.Text(), time.Now()) {
			fmt.Println(action)
		}
	}
	return scanner.Err()
}

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to JSON configuration")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: bt-device-recovery [--config path] {discover|watch|validate}")
		os.Exit(2)
	}
	var err error
	switch flag.Arg(0) {
	case "discover":
		err = discover()
	case "validate":
		_, err = loadConfig(filepath.Clean(*configPath))
	case "watch":
		err = watch(filepath.Clean(*configPath))
	default:
		err = fmt.Errorf("unknown command: %s", flag.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bt-device-recovery:", err)
		os.Exit(1)
	}
}
