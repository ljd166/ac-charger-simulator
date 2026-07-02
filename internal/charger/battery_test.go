package charger

import (
	"testing"
	"time"

	"github.com/ljd166/ac-charger-simulator/internal/config"
)

func batteryCfg(capacity, initSOC, targetSOC, timeScale float64) config.ChargerConfig {
	return config.ChargerConfig{
		ID: "BAT-01", ConnectorID: 1, Phase: "single",
		MaxCurrentA: 32, VoltageV: 230, PowerFactor: 1.0,
		BatteryCapacityKWh: capacity, InitialSOC: initSOC,
		TargetSOC: targetSOC, TimeScale: timeScale,
	}
}

// TestBattery_Defaults 未配置时使用默认电池参数
func TestBattery_Defaults(t *testing.T) {
	c := NewCharger(config.ChargerConfig{ID: "X", Phase: "single", MaxCurrentA: 32, VoltageV: 230, PowerFactor: 1})
	if c.batteryCapacityKWh != 55.0 {
		t.Fatalf("default capacity = %v, want 55", c.batteryCapacityKWh)
	}
	if c.soc != 20.0 {
		t.Fatalf("default initial soc = %v, want 20", c.soc)
	}
	if c.targetSOC != 90.0 {
		t.Fatalf("default target soc = %v, want 90", c.targetSOC)
	}
}

// TestBattery_SOCFromEnergy SOC 增长 = ΔE×效率/容量(物理一致,非拍脑袋)
func TestBattery_SOCFromEnergy(t *testing.T) {
	c := NewCharger(batteryCfg(50, 30, 90, 1))
	// 直接驱动内部结算逻辑:模拟桩表走了 5kWh
	c.mu.Lock()
	c.socEnergyBase = 0
	deltaE := 5.0
	c.soc += deltaE * chargeEfficiency / c.batteryCapacityKWh * 100.0
	c.mu.Unlock()
	// 30 + 5×0.92/50×100 = 30 + 9.2 = 39.2
	if got := c.Snapshot().SOC; got < 39.19 || got > 39.21 {
		t.Fatalf("soc = %v, want 39.2", got)
	}
}

// TestBattery_TaperCap CC-CV 锥形:<80% 不限;80-100% 线性降至 10%(下限 6A)
func TestBattery_TaperCap(t *testing.T) {
	c := NewCharger(batteryCfg(55, 20, 100, 1))
	cases := []struct {
		soc  float64
		want float64
	}{
		{50, 32},          // 未进入锥形
		{80, 32},          // 边界:满电流
		{90, 32 * 0.55},   // 中点:1-0.45=0.55
		{100, 6},          // 10%×32=3.2 → 下限 6A
	}
	for _, tc := range cases {
		c.mu.Lock()
		c.soc = tc.soc
		got := c.taperCurrentCap()
		c.mu.Unlock()
		if got < tc.want-0.01 || got > tc.want+0.01 {
			t.Fatalf("soc=%v: cap=%v, want %v", tc.soc, got, tc.want)
		}
	}
}

// TestBattery_SetSOC 置位并重置能量基准
func TestBattery_SetSOC(t *testing.T) {
	c := NewCharger(batteryCfg(55, 20, 90, 1))
	c.SetSOC(66.6)
	if got := c.Snapshot().SOC; got != 66.6 {
		t.Fatalf("soc = %v, want 66.6", got)
	}
	c.SetSOC(150) // 越界钳制
	if got := c.Snapshot().SOC; got != 100 {
		t.Fatalf("soc = %v, want 100", got)
	}
}

// TestBattery_TimeScale 时间倍率加速电表能量累计
func TestBattery_TimeScale(t *testing.T) {
	c := NewCharger(batteryCfg(55, 20, 90, 60)) // 60 倍速
	c.meter.SetTargetCurrent(32)
	base := time.Now()
	// 先收敛电流(多次小步)再累计 10 秒
	for i := 1; i <= 20; i++ {
		c.meter.Update(base.Add(time.Duration(i) * 500 * time.Millisecond))
	}
	snap := c.meter.Snapshot()
	// 真实 10s×60倍 = 600s 等效;32A×230V≈7.36kW → ≈7.36×600/3600≈1.2kWh 量级
	if snap.EnergyKWh < 0.5 {
		t.Fatalf("time-scaled energy = %v kWh, want >0.5 (accelerated)", snap.EnergyKWh)
	}
	// 对照:1 倍速同样步进能量应远小于
	c2 := NewCharger(batteryCfg(55, 20, 90, 1))
	c2.meter.SetTargetCurrent(32)
	for i := 1; i <= 20; i++ {
		c2.meter.Update(base.Add(time.Duration(i) * 500 * time.Millisecond))
	}
	if e2 := c2.meter.Snapshot().EnergyKWh; e2 >= snap.EnergyKWh/10 {
		t.Fatalf("1x energy %v should be ≪ 60x energy %v", e2, snap.EnergyKWh)
	}
}
