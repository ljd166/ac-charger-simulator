package meter

import (
	"math"
	"sync"
	"time"
)

// Model 模拟电表模型
type Model struct {
	mu sync.RWMutex

	phaseCount    int
	voltageV      float64
	powerFactor   float64
	maxCurrentA   float64
	
	targetCurrentA float64
	actualCurrentA float64
	currentL1      float64
	currentL2      float64
	currentL3      float64
	
	energyKWh float64
	lastUpdate time.Time
	paused    bool
	pauseReason string

	// timeScale 时间倍率(默认 1.0 真实速度;>1 加速能量累计与电流收敛,台架观察用)
	timeScale float64
}

// SetTimeScale 设置时间倍率(<=0 视为 1.0)
func (m *Model) SetTimeScale(scale float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if scale <= 0 {
		scale = 1.0
	}
	m.timeScale = scale
}

// NewModel 创建新电表模型
func NewModel(phaseCount int, voltageV, powerFactor, maxCurrentA float64) *Model {
	return &Model{
		phaseCount:  phaseCount,
		voltageV:    voltageV,
		powerFactor: powerFactor,
		maxCurrentA: maxCurrentA,
		lastUpdate:  time.Now(),
	}
}

// SetTargetCurrent 设置目标电流（来自 SetChargingProfile 或本地控制）
// 返回值：是否有效，是否低于6A暂停
func (m *Model) SetTargetCurrent(limitA float64) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 限制在 maxCurrentA 以内
	if limitA > m.maxCurrentA {
		limitA = m.maxCurrentA
	}

	if limitA < 6.0 {
		m.paused = true
		m.pauseReason = "below 6A threshold"
		m.targetCurrentA = 0
		return true, true
	}

	m.paused = false
	m.pauseReason = ""
	m.targetCurrentA = limitA
	return true, false
}

// SetTargetPower 设置目标功率（W 单位换算为电流）
func (m *Model) SetTargetPower(powerW float64) (bool, bool) {
	var currentA float64
	if m.phaseCount == 1 {
		currentA = powerW / (m.voltageV * m.powerFactor)
	} else {
		currentA = powerW / (1.732 * m.voltageV * m.powerFactor)
	}
	return m.SetTargetCurrent(currentA)
}

// Update 更新电表读数，返回当前实际电流和功率
func (m *Model) Update(now time.Time) (actualCurrentA, powerKW float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delta := now.Sub(m.lastUpdate).Seconds()
	if delta <= 0 {
		return m.actualCurrentA, m.getPower()
	}
	m.lastUpdate = now
	if m.timeScale > 1 {
		delta *= m.timeScale
	}

	if m.paused {
		// 电流收敛到 0
		m.actualCurrentA = m.converge(0, delta)
		m.currentL1 = m.actualCurrentA
		m.currentL2 = m.actualCurrentA
		m.currentL3 = m.actualCurrentA
	} else {
		// 电流收敛到目标电流
		m.actualCurrentA = m.converge(m.targetCurrentA, delta)
		m.currentL1 = m.actualCurrentA
		m.currentL2 = m.actualCurrentA
		m.currentL3 = m.actualCurrentA
	}

	// 计算功率
	powerKW = m.getPower()
	// 累计电量
	m.energyKWh += powerKW * (delta / 3600.0)

	return m.actualCurrentA, powerKW
}

// converge 电流收敛函数：在 1-2 个采样周期（约 5-10 秒）内收敛到目标
func (m *Model) converge(target, delta float64) float64 {
	const convergenceRate = 0.5 // 每秒收敛 50% 差距
	diff := target - m.actualCurrentA
	change := diff * convergenceRate * delta
	if math.Abs(change) > math.Abs(diff) {
		change = diff
	}
	return m.actualCurrentA + change
}

func (m *Model) getPower() float64 {
	if m.phaseCount == 1 {
		return m.voltageV * m.actualCurrentA * m.powerFactor / 1000.0
	}
	// 三相: 1.732 * U * I * PF / 1000
	return 1.732 * m.voltageV * m.actualCurrentA * m.powerFactor / 1000.0
}

// Snapshot 获取当前电表快照
func (m *Model) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{
		TargetCurrentA: m.targetCurrentA,
		ActualCurrentA: m.actualCurrentA,
		CurrentL1:      m.currentL1,
		CurrentL2:      m.currentL2,
		CurrentL3:      m.currentL3,
		PowerKW:        m.getPower(),
		EnergyKWh:      m.energyKWh,
		Paused:         m.paused,
		PauseReason:    m.pauseReason,
	}
}

// Snapshot 电表快照
type Snapshot struct {
	TargetCurrentA float64
	ActualCurrentA float64
	CurrentL1      float64
	CurrentL2      float64
	CurrentL3      float64
	PowerKW        float64
	EnergyKWh      float64
	Paused         bool
	PauseReason    string
}

// ResetEnergy 重置累计电量
func (m *Model) ResetEnergy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.energyKWh = 0
}

// ZeroFlow 立即归零电流/功率(停充/拔枪;Update 仅在充电时驱动,不归零会残留假功率)
func (m *Model) ZeroFlow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actualCurrentA = 0
	m.currentL1, m.currentL2, m.currentL3 = 0, 0, 0
}
