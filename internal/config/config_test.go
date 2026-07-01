package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	data := `
web_console:
  enabled: true
  bind_addr: "127.0.0.1"
  port: 8088
chargers:
  - id: "SIM-AC-001"
    connector_id: 1
    endpoint: "ws://127.0.0.1:9000/ocpp/SIM-AC-001"
    id_tag: "TEST-CARD-001"
    phase: "single"
    phase_assignment: "L1"
    max_current_a: 32
    voltage_v: 230
    power_factor: 0.98
    meter_interval_sec: 5
  - id: "SIM-AC-002"
    connector_id: 1
    endpoint: "ws://127.0.0.1:9000/ocpp/SIM-AC-002"
    id_tag: "TEST-CARD-002"
    phase: "three"
    phase_assignment: "L1L2L3"
    max_current_a: 16
    voltage_v: 230
    power_factor: 0.98
    meter_interval_sec: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Chargers) != 2 {
		t.Fatalf("expected 2 chargers, got %d", len(cfg.Chargers))
	}
	if cfg.Chargers[0].ID != "SIM-AC-001" {
		t.Fatalf("expected SIM-AC-001, got %s", cfg.Chargers[0].ID)
	}
	if cfg.Chargers[0].Phase != "single" {
		t.Fatalf("expected single, got %s", cfg.Chargers[0].Phase)
	}
	if cfg.Chargers[1].Phase != "three" {
		t.Fatalf("expected three, got %s", cfg.Chargers[1].Phase)
	}
	if cfg.WebConsole.Port != 8088 {
		t.Fatalf("expected port 8088, got %d", cfg.WebConsole.Port)
	}
}

func TestLoadMissingID(t *testing.T) {
	data := `
chargers:
  - connector_id: 1
    endpoint: "ws://127.0.0.1:9000/ocpp/1"
    max_current_a: 32
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestLoadMissingEndpoint(t *testing.T) {
	data := `
chargers:
  - id: "SIM-AC-001"
    max_current_a: 32
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestLoadInvalidPhase(t *testing.T) {
	data := `
chargers:
  - id: "SIM-AC-001"
    endpoint: "ws://127.0.0.1:9000/ocpp/1"
    phase: "two"
    max_current_a: 32
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid phase")
	}
}

func TestLoadDuplicateID(t *testing.T) {
	data := `
chargers:
  - id: "SIM-AC-001"
    endpoint: "ws://127.0.0.1:9000/ocpp/1"
    max_current_a: 32
  - id: "SIM-AC-001"
    endpoint: "ws://127.0.0.1:9000/ocpp/2"
    max_current_a: 32
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoadDefaults(t *testing.T) {
	data := `
chargers:
  - id: "SIM-AC-001"
    endpoint: "ws://127.0.0.1:9000/ocpp/1"
    phase: "single"
    max_current_a: 32
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ch := cfg.Chargers[0]
	if ch.VoltageV != 230 {
		t.Fatalf("expected voltage 230, got %f", ch.VoltageV)
	}
	if ch.PowerFactor != 0.98 {
		t.Fatalf("expected power_factor 0.98, got %f", ch.PowerFactor)
	}
	if ch.MeterIntervalSec != 5 {
		t.Fatalf("expected meter_interval_sec 5, got %d", ch.MeterIntervalSec)
	}
	if ch.ConnectorID != 1 {
		t.Fatalf("expected connector_id 1, got %d", ch.ConnectorID)
	}
	if ch.IDTag != "TEST-CARD-SIM-AC-001" {
		t.Fatalf("expected id_tag TEST-CARD-SIM-AC-001, got %s", ch.IDTag)
	}
}
