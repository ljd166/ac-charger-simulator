package telemetry

import (
	"sync"
	"time"

	"github.com/ljd166/ac-charger-simulator/internal/charger"
)

// Hub 负责聚合所有桩的遥测数据并广播
type Hub struct {
	mu sync.RWMutex

	// 最新遥测
	latest map[string]charger.Telemetry

	// 历史曲线
	history map[string][]HistoryPoint

	// 事件日志
	events []EventRecord

	// 最大事件数
	maxEvents int

	// 最大历史点数
	maxHistory int

	// WebSocket 订阅者
	subscribers map[string]chan Broadcast

	// 全局运行时间
	startTime time.Time
}

// HistoryPoint 历史曲线点
type HistoryPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	CurrentA   float64   `json:"current_a"`
	PowerKW    float64   `json:"power_kw"`
	SOC        float64   `json:"soc"`
}

// EventRecord 事件记录
type EventRecord struct {
	Timestamp time.Time `json:"timestamp"`
	ChargerID string    `json:"charger_id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
}

// Broadcast 广播消息
type Broadcast struct {
	Type      string             `json:"type"`
	ChargerID string             `json:"charger_id,omitempty"`
	Telemetry charger.Telemetry  `json:"telemetry,omitempty"`
	Event     EventRecord        `json:"event,omitempty"`
	State     GlobalState        `json:"state,omitempty"`
}

// GlobalState 全局状态
type GlobalState struct {
	Timestamp        time.Time `json:"timestamp"`
	RunTimeSec       int64     `json:"run_time_sec"`
	OCppEndpoint     string    `json:"ocpp_endpoint"`
	ConfiguredCount  int       `json:"configured_count"`
	ConnectedCount   int       `json:"connected_count"`
	ChargingCount    int       `json:"charging_count"`
	RecentError      string    `json:"recent_error,omitempty"`
}

// NewHub 创建新 telemetry hub
func NewHub() *Hub {
	return &Hub{
		latest:      make(map[string]charger.Telemetry),
		history:     make(map[string][]HistoryPoint),
		events:      make([]EventRecord, 0, 1000),
		maxEvents:   1000,
		maxHistory:  600,
		subscribers: make(map[string]chan Broadcast),
		startTime:   time.Now(),
	}
}

// OnTelemetry 处理单个桩的遥测数据
func (h *Hub) OnTelemetry(id string, snap charger.Telemetry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.latest[id] = snap

	// 追加历史
	points := h.history[id]
	points = append(points, HistoryPoint{
		Timestamp: snap.Timestamp,
		CurrentA:  snap.ActualCurrentA,
		PowerKW:   snap.PowerKW,
		SOC:       snap.SOC,
	})
	if len(points) > h.maxHistory {
		points = points[len(points)-h.maxHistory:]
	}
	h.history[id] = points

	// 广播
	h.broadcast(Broadcast{
		Type:      "telemetry",
		ChargerID: id,
		Telemetry: snap,
	})
}

// OnEvent 处理事件
func (h *Hub) OnEvent(id string, event charger.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rec := EventRecord{
		Timestamp: event.Timestamp,
		ChargerID: id,
		Type:      event.Type,
		Message:   event.Message,
	}
	h.events = append(h.events, rec)
	if len(h.events) > h.maxEvents {
		h.events = h.events[len(h.events)-h.maxEvents:]
	}

	h.broadcast(Broadcast{
		Type:  "event",
		Event: rec,
	})
}

// OnStateChange 处理状态变化
func (h *Hub) OnStateChange(id string, oldState, newState string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rec := EventRecord{
		Timestamp: time.Now(),
		ChargerID: id,
		Type:      "state_change",
		Message:   oldState + " -> " + newState,
	}
	h.events = append(h.events, rec)
	if len(h.events) > h.maxEvents {
		h.events = h.events[len(h.events)-h.maxEvents:]
	}

	h.broadcast(Broadcast{
		Type:  "event",
		Event: rec,
	})
}

// Subscribe 订阅广播
func (h *Hub) Subscribe(id string) chan Broadcast {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan Broadcast, 10)
	h.subscribers[id] = ch
	return ch
}

// Unsubscribe 取消订阅
func (h *Hub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ch, ok := h.subscribers[id]; ok {
		close(ch)
		delete(h.subscribers, id)
	}
}

// broadcast 向所有订阅者广播
func (h *Hub) broadcast(msg Broadcast) {
	for id, ch := range h.subscribers {
		select {
		case ch <- msg:
		default:
			// 通道满，跳过
			_ = id
		}
	}
}

// Latest 返回所有最新遥测
func (h *Hub) Latest() map[string]charger.Telemetry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	result := make(map[string]charger.Telemetry, len(h.latest))
	for k, v := range h.latest {
		result[k] = v
	}
	return result
}

// History 返回指定桩的历史曲线
func (h *Hub) History(chargerID string) []HistoryPoint {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	points := h.history[chargerID]
	result := make([]HistoryPoint, len(points))
	copy(result, points)
	return result
}

// Events 返回最近事件
func (h *Hub) Events(limit int) []EventRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	if limit <= 0 || limit > len(h.events) {
		limit = len(h.events)
	}
	start := len(h.events) - limit
	if start < 0 {
		start = 0
	}
	result := make([]EventRecord, limit)
	copy(result, h.events[start:])
	return result
}

// GlobalState 返回全局状态
func (h *Hub) GlobalState(endpoint string, configured, connected, charging int) GlobalState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	return GlobalState{
		Timestamp:       time.Now(),
		RunTimeSec:      int64(time.Since(h.startTime).Seconds()),
		OCppEndpoint:    endpoint,
		ConfiguredCount: configured,
		ConnectedCount:  connected,
		ChargingCount:   charging,
	}
}
