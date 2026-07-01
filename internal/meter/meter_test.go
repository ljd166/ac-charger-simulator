package meter

import (
	"testing"
	"time"
)

func TestSinglePhasePower(t *testing.T) {
	m := NewModel(1, 230, 0.98, 32)
	m.SetTargetCurrent(16)
	
	now := time.Now()
	// 模拟 1 秒更新
	actual, power := m.Update(now.Add(1 * time.Second))
	if actual <= 0 {
		t.Fatalf("expected actual current > 0, got %f", actual)
	}
	// 单相功率: 230 * I * 0.98 / 1000
	expectedPower := 230.0 * actual * 0.98 / 1000.0
	if power != expectedPower {
		t.Fatalf("expected power %f, got %f", expectedPower, power)
	}
}

func TestThreePhasePower(t *testing.T) {
	m := NewModel(3, 230, 0.98, 16)
	m.SetTargetCurrent(16)
	
	now := time.Now()
	actual, power := m.Update(now.Add(1 * time.Second))
	if actual <= 0 {
		t.Fatalf("expected actual current > 0, got %f", actual)
	}
	// 三相功率: 1.732 * 230 * I * 0.98 / 1000
	expectedPower := 1.732 * 230.0 * actual * 0.98 / 1000.0
	if power != expectedPower {
		t.Fatalf("expected power %f, got %f", expectedPower, power)
	}
}

func TestSetTargetCurrentBelow6A(t *testing.T) {
	m := NewModel(1, 230, 0.98, 32)
	ok, paused := m.SetTargetCurrent(5)
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if !paused {
		t.Fatal("expected paused to be true")
	}
	now := time.Now()
	actual, _ := m.Update(now.Add(1 * time.Second))
	if actual != 0 {
		t.Fatalf("expected 0 current when paused, got %f", actual)
	}
}

func TestSetTargetCurrentExceedsMax(t *testing.T) {
	m := NewModel(1, 230, 0.98, 32)
	ok, paused := m.SetTargetCurrent(50)
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if paused {
		t.Fatal("expected paused to be false")
	}
	// 目标电流应被限制为 32
	snap := m.Snapshot()
	if snap.TargetCurrentA != 32 {
		t.Fatalf("expected target current 32, got %f", snap.TargetCurrentA)
	}
}

func TestConvergence(t *testing.T) {
	m := NewModel(1, 230, 0.98, 32)
	m.SetTargetCurrent(16)
	
	now := time.Now()
	// 第一次更新，应收敛一部分
	actual1, _ := m.Update(now.Add(1 * time.Second))
	// 第二次更新，应更接近目标
	actual2, _ := m.Update(now.Add(2 * time.Second))
	if actual2 <= actual1 {
		t.Fatalf("expected current to increase, got %f -> %f", actual1, actual2)
	}
	
	// 经过足够多次更新，应收敛到目标
	for i := 0; i < 20; i++ {
		actual2, _ = m.Update(now.Add(time.Duration(i+3) * time.Second))
	}
	if actual2 < 15.5 || actual2 > 16.5 {
		t.Fatalf("expected current near 16, got %f", actual2)
	}
}

func TestSetTargetPower(t *testing.T) {
	// 单相: P=230*I*0.98, 目标 3680W => I=16.32A
	m := NewModel(1, 230, 0.98, 32)
	ok, paused := m.SetTargetPower(3680)
	if !ok || paused {
		t.Fatal("expected ok and not paused")
	}
	
	// 三相: P=1.732*230*I*0.98, 目标 6270W => I=16A
	m2 := NewModel(3, 230, 0.98, 16)
	ok, paused = m2.SetTargetPower(6270)
	if !ok || paused {
		t.Fatal("expected ok and not paused")
	}
}

func TestEnergyAccumulation(t *testing.T) {
	m := NewModel(1, 230, 0.98, 32)
	m.SetTargetCurrent(16)
	
	now := time.Now()
	// 模拟 10 秒
	for i := 0; i < 10; i++ {
		m.Update(now.Add(time.Duration(i+1) * time.Second))
	}
	
	snap := m.Snapshot()
	if snap.EnergyKWh <= 0 {
		t.Fatalf("expected energy > 0, got %f", snap.EnergyKWh)
	}
}
