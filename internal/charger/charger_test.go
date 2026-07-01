package charger

import (
	"testing"
	"time"

	"github.com/ljd166/ac-charger-simulator/internal/config"
)

func newTestCharger() *Charger {
	cfg := config.ChargerConfig{
		ID:              "TEST-001",
		ConnectorID:     1,
		Endpoint:        "ws://127.0.0.1:9999/ocpp/TEST-001",
		IDTag:           "TEST-CARD",
		Phase:           "single",
		PhaseAssignment: "L1",
		MaxCurrentA:     32,
		VoltageV:        230,
		PowerFactor:     0.98,
		MeterIntervalSec: 5,
	}
	return NewCharger(cfg)
}

func TestChargerStatusTransitions(t *testing.T) {
	c := newTestCharger()
	if c.Status() != Available {
		t.Fatalf("expected Available, got %s", c.Status())
	}
	// 未连接时不能启动
	if err := c.Start(); err == nil {
		t.Fatal("expected error when not connected")
	}
	// 故障状态
	c.SetFault("EarthFailure")
	if c.Status() != Faulted {
		t.Fatalf("expected Faulted, got %s", c.Status())
	}
	c.ClearFault()
	if c.Status() != Available {
		t.Fatalf("expected Available after clear fault, got %s", c.Status())
	}
}

func TestChargerSetTargetCurrent(t *testing.T) {
	c := newTestCharger()
	if err := c.SetTargetCurrent(16); err != nil {
		t.Fatalf("set target current: %v", err)
	}
	if c.TargetCurrentA() != 16 {
		t.Fatalf("expected 16A, got %f", c.TargetCurrentA())
	}
	// 超过 max_current_a 应被限制
	if err := c.SetTargetCurrent(50); err != nil {
		t.Fatalf("set target current: %v", err)
	}
	if c.TargetCurrentA() != 32 {
		t.Fatalf("expected 32A, got %f", c.TargetCurrentA())
	}
	// 低于 6A 应暂停
	if err := c.SetTargetCurrent(5); err != nil {
		t.Fatalf("set target current: %v", err)
	}
	if c.Status() != SuspendedEVSE {
		t.Fatalf("expected SuspendedEVSE, got %s", c.Status())
	}
}

func TestChargerSetTargetPower(t *testing.T) {
	c := newTestCharger()
	// 单相: P = 230 * I * 0.98 => I = 3680/(230*0.98) = 16.32A
	if err := c.SetTargetPower(3680); err != nil {
		t.Fatalf("set target power: %v", err)
	}
	// 目标电流应接近 16.32A
	if c.TargetCurrentA() < 16 || c.TargetCurrentA() > 17 {
		t.Fatalf("expected current near 16.3A, got %f", c.TargetCurrentA())
	}
}

func TestChargerSingleFaultNoImpact(t *testing.T) {
	c1 := newTestCharger()
	c2 := newTestCharger()
	c2.config.ID = "TEST-002"
	
	c1.SetFault("EarthFailure")
	if c1.Status() != Faulted {
		t.Fatalf("expected c1 Faulted, got %s", c1.Status())
	}
	if c2.Status() != Available {
		t.Fatalf("expected c2 Available, got %s", c2.Status())
	}
}

func TestChargerSnapshot(t *testing.T) {
	c := newTestCharger()
	snap := c.Snapshot()
	if snap.ConnectionState != Disconnected {
		t.Fatalf("expected disconnected, got %s", snap.ConnectionState)
	}
	if snap.Status != Available {
		t.Fatalf("expected Available, got %s", snap.Status)
	}
	if snap.MaxCurrentA != 32 {
		t.Fatalf("expected max 32A, got %f", snap.MaxCurrentA)
	}
	if snap.PhaseCount != 1 {
		t.Fatalf("expected phase 1, got %d", snap.PhaseCount)
	}
}

func TestChargerSetTargetCurrentThreePhase(t *testing.T) {
	cfg := config.ChargerConfig{
		ID:              "TEST-003",
		ConnectorID:     1,
		Endpoint:        "ws://127.0.0.1:9999/ocpp/TEST-003",
		IDTag:           "TEST-CARD",
		Phase:           "three",
		PhaseAssignment: "L1L2L3",
		MaxCurrentA:     16,
		VoltageV:        230,
		PowerFactor:     0.98,
		MeterIntervalSec: 5,
	}
	c := NewCharger(cfg)
	if err := c.SetTargetPower(6270); err != nil {
		t.Fatalf("set target power: %v", err)
	}
	// 三相: P = 1.732 * 230 * I * 0.98 => I = 6270/(1.732*230*0.98) = 16A
	if c.TargetCurrentA() < 15.5 || c.TargetCurrentA() > 16.5 {
		t.Fatalf("expected current near 16A, got %f", c.TargetCurrentA())
	}
}

func TestChargerCallbacks(t *testing.T) {
	c := newTestCharger()
	var eventReceived bool
	c.SetCallbacks(
		func(id string, oldState, newState string) {},
		func(id string, snap Telemetry) {},
		func(id string, event Event) {
			if event.Type == "fault" {
				eventReceived = true
			}
		},
	)
	c.SetFault("TestFault")
	// 给回调一点时间
	time.Sleep(50 * time.Millisecond)
	if !eventReceived {
		t.Fatal("expected event callback to be triggered")
	}
}
