package manager

import (
	"testing"

	"github.com/ljd166/ac-charger-simulator/internal/config"
	"github.com/ljd166/ac-charger-simulator/internal/telemetry"
)

func TestNewManager(t *testing.T) {
	cfg := &config.SimulatorConfig{
		Chargers: []config.ChargerConfig{
			{ID: "SIM-001", Endpoint: "ws://127.0.0.1:9000/ocpp/1", Phase: "single", MaxCurrentA: 32},
			{ID: "SIM-002", Endpoint: "ws://127.0.0.1:9000/ocpp/2", Phase: "three", MaxCurrentA: 16},
		},
	}
	hub := telemetry.NewHub()
	mgr := NewManager(cfg, hub)

	if len(mgr.AllChargers()) != 2 {
		t.Fatalf("expected 2 chargers, got %d", len(mgr.AllChargers()))
	}
	c, ok := mgr.GetCharger("SIM-001")
	if !ok {
		t.Fatal("expected charger SIM-001")
	}
	if c.ID() != "SIM-001" {
		t.Fatalf("expected ID SIM-001, got %s", c.ID())
	}
}

func TestManagerSetEndpoint(t *testing.T) {
	cfg := &config.SimulatorConfig{
		Chargers: []config.ChargerConfig{
			{ID: "SIM-001", Endpoint: "ws://old:9000/ocpp/1", Phase: "single", MaxCurrentA: 32},
		},
	}
	hub := telemetry.NewHub()
	mgr := NewManager(cfg, hub)
	mgr.SetEndpoint("ws://new:9000")
	if mgr.GetEndpoint() != "ws://new:9000" {
		t.Fatalf("expected endpoint ws://new:9000, got %s", mgr.GetEndpoint())
	}
}
