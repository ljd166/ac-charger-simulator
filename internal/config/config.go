package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SimulatorConfig 是模拟器全局配置
type SimulatorConfig struct {
	WebConsole WebConsoleConfig `yaml:"web_console"`
	Chargers   []ChargerConfig  `yaml:"chargers"`
}

// WebConsoleConfig Web Console 配置
type WebConsoleConfig struct {
	Enabled          bool   `yaml:"enabled"`
	BindAddr         string `yaml:"bind_addr"`
	Port             int    `yaml:"port"`
	HistoryWindowSec int    `yaml:"history_window_sec"`
}

// ChargerConfig 单桩配置
type ChargerConfig struct {
	ID                string  `yaml:"id"`
	ConnectorID       int     `yaml:"connector_id"`
	Endpoint          string  `yaml:"endpoint"`
	IDTag             string  `yaml:"id_tag"`
	Phase             string  `yaml:"phase"`
	PhaseAssignment   string  `yaml:"phase_assignment"`
	MaxCurrentA       float64 `yaml:"max_current_a"`
	VoltageV          float64 `yaml:"voltage_v"`
	PowerFactor       float64 `yaml:"power_factor"`
	MeterIntervalSec  int     `yaml:"meter_interval_sec"`

	// 电池模型(SOC 由充电能量物理驱动)
	BatteryCapacityKWh float64 `yaml:"battery_capacity_kwh"` // 默认 55(Model 3 SR)
	InitialSOC         float64 `yaml:"initial_soc"`          // 默认 20(%)
	TargetSOC          float64 `yaml:"target_soc"`           // 默认 90(%);到达自动停充(仿真真车行为)
	TimeScale          float64 `yaml:"time_scale"`           // 默认 1.0 真实速度;>1 加速电池动态与计量(台架观察用)
}

// DefaultWebConsole 返回默认 Web Console 配置
func DefaultWebConsole() WebConsoleConfig {
	return WebConsoleConfig{
		Enabled:          true,
		BindAddr:         "127.0.0.1",
		Port:             8088,
		HistoryWindowSec: 600,
	}
}

// Load 从文件加载配置
func Load(path string) (*SimulatorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg SimulatorConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.WebConsole.BindAddr == "" {
		cfg.WebConsole = DefaultWebConsole()
	}
	if cfg.WebConsole.HistoryWindowSec == 0 {
		cfg.WebConsole.HistoryWindowSec = 600
	}
	return &cfg, nil
}

// Validate 校验配置
func (c *SimulatorConfig) Validate() error {
	if len(c.Chargers) == 0 {
		return fmt.Errorf("no chargers configured")
	}
	if len(c.Chargers) > 16 {
		return fmt.Errorf("too many chargers: max 16, got %d", len(c.Chargers))
	}
	ids := make(map[string]struct{})
	for i, ch := range c.Chargers {
		if ch.ID == "" {
			return fmt.Errorf("charger %d: id is required", i)
		}
		if _, ok := ids[ch.ID]; ok {
			return fmt.Errorf("charger %d: duplicate id %q", i, ch.ID)
		}
		ids[ch.ID] = struct{}{}
		if ch.Endpoint == "" {
			return fmt.Errorf("charger %d: endpoint is required", i)
		}
		if ch.Phase != "single" && ch.Phase != "three" {
			return fmt.Errorf("charger %d: phase must be single or three, got %q", i, ch.Phase)
		}
		if ch.MaxCurrentA <= 0 {
			return fmt.Errorf("charger %d: max_current_a must be > 0", i)
		}
		if ch.VoltageV <= 0 {
			ch.VoltageV = 230
		}
		if ch.PowerFactor <= 0 || ch.PowerFactor > 1 {
			ch.PowerFactor = 0.98
		}
		if ch.MeterIntervalSec <= 0 {
			ch.MeterIntervalSec = 5
		}
		if ch.ConnectorID <= 0 {
			ch.ConnectorID = 1
		}
		if ch.IDTag == "" {
			ch.IDTag = "TEST-CARD-" + ch.ID
		}
		c.Chargers[i] = ch
	}
	return nil
}
