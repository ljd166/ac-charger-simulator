package manager

import (
	"log"
	"sync"

	"github.com/ljd166/ac-charger-simulator/internal/charger"
	"github.com/ljd166/ac-charger-simulator/internal/config"
	"github.com/ljd166/ac-charger-simulator/internal/telemetry"
)

// Manager 管理所有模拟桩
type Manager struct {
	mu       sync.RWMutex
	chargers map[string]*charger.Charger
	endpoint string
}

// NewManager 创建新管理器
func NewManager(cfg *config.SimulatorConfig, hub *telemetry.Hub) *Manager {
	m := &Manager{
		chargers: make(map[string]*charger.Charger),
		endpoint: cfg.Chargers[0].Endpoint,
	}
	for _, chCfg := range cfg.Chargers {
		c := charger.NewCharger(chCfg)
		c.SetCallbacks(
			hub.OnStateChange,
			hub.OnTelemetry,
			hub.OnEvent,
		)
		m.chargers[c.ID()] = c
	}
	return m
}

// GetCharger 获取指定桩
func (m *Manager) GetCharger(id string) (*charger.Charger, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.chargers[id]
	return c, ok
}

// AllChargers 获取所有桩
func (m *Manager) AllChargers() []*charger.Charger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*charger.Charger, 0, len(m.chargers))
	for _, c := range m.chargers {
		result = append(result, c)
	}
	return result
}

// SetEndpoint 设置全局 OCPP endpoint
func (m *Manager) SetEndpoint(endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoint = endpoint
	for _, c := range m.chargers {
		c.ResetEndpoint(endpoint)
	}
	log.Printf("[Manager] global endpoint set to %s", endpoint)
}

// GetEndpoint 获取全局 endpoint
func (m *Manager) GetEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.endpoint
}

// StartAll 连接所有桩
func (m *Manager) StartAll() {
	for _, c := range m.AllChargers() {
		_ = c.Connect()
	}
}

// StopAll 断开所有桩
func (m *Manager) StopAll() {
	for _, c := range m.AllChargers() {
		c.Disconnect()
	}
}

// StartAllCharging 启动所有桩交易
func (m *Manager) StartAllCharging() {
	for _, c := range m.AllChargers() {
		_ = c.Start()
	}
}

// StopAllCharging 停止所有桩交易
func (m *Manager) StopAllCharging() {
	for _, c := range m.AllChargers() {
		_ = c.Stop()
	}
}
