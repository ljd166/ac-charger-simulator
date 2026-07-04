package charger

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ljd166/ac-charger-simulator/internal/config"
	"github.com/ljd166/ac-charger-simulator/internal/meter"
	"github.com/ljd166/ac-charger-simulator/internal/ocpp16"
)

// Status 表示连接器状态
type Status string

const (
	Available     Status = "Available"
	Preparing     Status = "Preparing"
	Charging      Status = "Charging"
	SuspendedEVSE Status = "SuspendedEVSE"
	SuspendedEV   Status = "SuspendedEV"
	Finishing     Status = "Finishing"
	Faulted       Status = "Faulted"
	Unavailable   Status = "Unavailable"
)

// ConnectionState 表示 WebSocket 连接状态
type ConnectionState string

const (
	Disconnected ConnectionState = "disconnected"
	Connecting   ConnectionState = "connecting"
	Connected    ConnectionState = "connected"
	Error        ConnectionState = "error"
)

// Charger 表示单桩模拟器
type Charger struct {
	mu sync.RWMutex

	config   config.ChargerConfig
	meter    *meter.Model

	connectionState ConnectionState
	status          Status
	transactionID   int
	faultCode       string

	targetCurrentA float64
	actualCurrentA float64
	powerKW        float64
	energyKWh      float64
	soc            float64
	profile        string
	profileStart   time.Time

	conn        *websocket.Conn
	writeMu     sync.Mutex
	sendCh      chan []byte
	stopCh      chan struct{}
	heartbeatInterval time.Duration

	// OCPP 待处理消息 ID
	pendingAuthorizeMsgID string
	pendingStartMsgID     string
	startPending          bool

	// 自动重连(非人为断开后退避重连;CSMS 重启不再永久卡 error)
	manualStop     bool
	reconnectDelay time.Duration

	// 电池模型:SOC 由充电能量物理驱动(soc += ΔE×效率/容量)
	batteryCapacityKWh float64
	targetSOC          float64
	socEnergyBase      float64 // 上次 SOC 结算时的桩表累计电量(kWh)
	fullStopSent       bool    // 到达目标 SOC 的自动停充只触发一次

	// 事件回调
	onStateChange func(id string, oldState, newState string)
	onTelemetry   func(id string, snap Telemetry)
	onEvent       func(id string, event Event)
}

// Telemetry 实时遥测数据
type Telemetry struct {
	Timestamp           time.Time       `json:"timestamp"`
	ConnectionState     ConnectionState `json:"ocpp_connection_state"`
	Status              Status          `json:"charger_status"`
	TransactionID       int             `json:"transaction_id"`
	TargetCurrentA      float64         `json:"target_current_a"`
	ActualCurrentA      float64         `json:"actual_current_a"`
	PowerKW             float64         `json:"power_kw"`
	EnergyKWh           float64         `json:"energy_kwh"`
	SOC                 float64         `json:"soc"`
	FaultCode           string          `json:"fault_code"`
	PhaseCount          int             `json:"phase_count"`
	VoltageV            float64         `json:"voltage_v"`
	MaxCurrentA         float64         `json:"max_current_a"`
	PhaseAssignment     string          `json:"phase_assignment"`
	BatteryCapacityKWh  float64         `json:"battery_capacity_kwh"`
	TargetSOC           float64         `json:"target_soc"`
}

// Event 事件日志条目
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
}

// NewCharger 创建新模拟桩
func NewCharger(cfg config.ChargerConfig) *Charger {
	phaseCount := 1
	if cfg.Phase == "three" {
		phaseCount = 3
	}
	if cfg.MeterIntervalSec <= 0 {
		cfg.MeterIntervalSec = 5
	}
	// 电池模型默认值
	capacity := cfg.BatteryCapacityKWh
	if capacity <= 0 {
		capacity = 55.0 // Tesla Model 3 SR
	}
	initialSOC := cfg.InitialSOC
	if initialSOC <= 0 || initialSOC > 100 {
		initialSOC = 20.0
	}
	targetSOC := cfg.TargetSOC
	if targetSOC <= 0 || targetSOC > 100 {
		targetSOC = 90.0
	}

	c := &Charger{
		config:             cfg,
		meter:              meter.NewModel(phaseCount, cfg.VoltageV, cfg.PowerFactor, cfg.MaxCurrentA),
		connectionState:    Disconnected,
		status:             Available,
		faultCode:          "NoError",
		soc:                initialSOC,
		batteryCapacityKWh: capacity,
		targetSOC:          targetSOC,
		stopCh:             make(chan struct{}),
		sendCh:             make(chan []byte, 10),
		heartbeatInterval:  60 * time.Second,
	}
	c.meter.SetTimeScale(cfg.TimeScale)
	c.targetCurrentA = cfg.MaxCurrentA
	c.meter.SetTargetCurrent(cfg.MaxCurrentA)
	return c
}

// chargeEfficiency 桩表→电池的充电效率(AC 车载充电机损耗)
const chargeEfficiency = 0.92

// taperCurrentCap CC-CV 锥形:SOC≥80% 后电流上限线性锥减(80%→满 100%→10%,下限 6A)
func (c *Charger) taperCurrentCap() float64 {
	if c.soc < 80 {
		return c.config.MaxCurrentA
	}
	frac := 1.0 - (c.soc-80.0)/20.0*0.9 // 80%:1.0 → 100%:0.1
	cap := c.config.MaxCurrentA * frac
	if cap < 6.0 {
		cap = 6.0
	}
	return cap
}

// SetSOC 直接设定电池 SOC(测试复位用),并重置能量结算基准
func (c *Charger) SetSOC(soc float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if soc < 0 {
		soc = 0
	}
	if soc > 100 {
		soc = 100
	}
	c.soc = soc
	c.socEnergyBase = c.meter.Snapshot().EnergyKWh
	c.emitEvent("config", fmt.Sprintf("SOC reset to %.1f%%", soc))
}

// ID 返回桩 ID
func (c *Charger) ID() string { return c.config.ID }

// Config 返回配置
func (c *Charger) Config() config.ChargerConfig { return c.config }

// SetCallbacks 设置事件回调
func (c *Charger) SetCallbacks(onStateChange func(id string, oldState, newState string), onTelemetry func(id string, snap Telemetry), onEvent func(id string, event Event)) {
	c.onStateChange = onStateChange
	c.onTelemetry = onTelemetry
	c.onEvent = onEvent
}

// Connect 连接 OCPP endpoint
func (c *Charger) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connectionState == Connected || c.connectionState == Connecting {
		return fmt.Errorf("already connected or connecting")
	}
	c.connectionState = Connecting
	c.manualStop = false
	c.reconnectDelay = 0
	c.emitStateChange(Disconnected, Connecting)
	c.emitEvent("connect", fmt.Sprintf("connecting to %s", c.config.Endpoint))

	go c.run()
	return nil
}

// scheduleReconnect 非人为断开后的自动重连(退避 5s→30s)。
// 解决 CSMS(R3S)重启后模拟桩永久卡在 error 需人工 /connect 的问题。
func (c *Charger) scheduleReconnect() {
	c.mu.Lock()
	if c.reconnectDelay < 5*time.Second {
		c.reconnectDelay = 5 * time.Second
	} else if c.reconnectDelay < 30*time.Second {
		c.reconnectDelay += 5 * time.Second
	}
	delay := c.reconnectDelay
	c.mu.Unlock()

	time.Sleep(delay)

	c.mu.Lock()
	if c.manualStop || c.connectionState == Connected || c.connectionState == Connecting {
		c.mu.Unlock()
		return
	}
	oldState := c.connectionState
	c.connectionState = Connecting
	c.emitStateChange(oldState, Connecting)
	c.emitEvent("reconnect", fmt.Sprintf("auto-reconnecting after %v", delay))
	c.mu.Unlock()
	go c.run()
}

// Disconnect 断开连接
func (c *Charger) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnectLocked()
}

func (c *Charger) disconnectLocked() {
	c.manualStop = true // 人为断开:抑制自动重连
	c.writeMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.writeMu.Unlock()
	close(c.stopCh)
	c.stopCh = make(chan struct{})
	oldState := c.connectionState
	c.connectionState = Disconnected
	c.emitStateChange(oldState, Disconnected)
	c.emitEvent("disconnect", "disconnected")
}

// Start 启动交易（模拟插枪+授权+启动）
func (c *Charger) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.status != Available && c.status != Preparing {
		return fmt.Errorf("cannot start from status %s", c.status)
	}
	if c.connectionState != Connected {
		return fmt.Errorf("not connected")
	}
	if c.startPending {
		return fmt.Errorf("start already pending")
	}

	c.startPending = true
	c.fullStopSent = false // 新一次充电,重置"充满自动停"标志
	c.setStatus(Preparing)
	c.sendStatusNotification()

	// 发送 Authorize，等待 CSMS 确认后再发送 StartTransaction
	c.sendAuthorize()
	c.emitEvent("start", "Authorize sent, waiting for CSMS response")
	return nil
}

// Stop 停止交易
func (c *Charger) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.status != Charging && c.status != SuspendedEVSE && c.status != SuspendedEV {
		return fmt.Errorf("cannot stop from status %s", c.status)
	}
	if c.connectionState != Connected {
		return fmt.Errorf("not connected")
	}

	c.sendStopTransaction()
	c.setStatus(Finishing)
	c.sendStatusNotification()
	c.transactionID = 0
	// 停充即断流:归零电流/功率,避免残留假读数
	c.meter.ZeroFlow()
	c.actualCurrentA = 0
	c.powerKW = 0
	c.setStatus(Available)
	c.sendStatusNotification()
	c.emitEvent("stop", "transaction stopped")
	return nil
}

// SetTargetCurrent 设置目标电流
func (c *Charger) SetTargetCurrent(limitA float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if limitA > c.config.MaxCurrentA {
		limitA = c.config.MaxCurrentA
	}
	ok, paused := c.meter.SetTargetCurrent(limitA)
	if !ok {
		return fmt.Errorf("failed to set target current")
	}
	c.targetCurrentA = limitA
	if paused {
		c.setStatus(SuspendedEVSE)
		c.sendStatusNotification()
		c.emitEvent("profile", fmt.Sprintf("target current %.1fA below 6A, suspended", limitA))
	} else if c.status == SuspendedEVSE {
		c.setStatus(Charging)
		c.sendStatusNotification()
	}
	c.emitEvent("profile", fmt.Sprintf("target current set to %.1fA", limitA))
	return nil
}

// SetTargetPower 设置目标功率
func (c *Charger) SetTargetPower(powerW float64) error {
	var currentA float64
	if c.config.Phase == "single" {
		currentA = powerW / (c.config.VoltageV * c.config.PowerFactor)
	} else {
		currentA = powerW / (1.732 * c.config.VoltageV * c.config.PowerFactor)
	}
	return c.SetTargetCurrent(currentA)
}

// SetFault 触发故障
func (c *Charger) SetFault(code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.faultCode = code
	oldStatus := c.status
	c.setStatus(Faulted)
	c.sendStatusNotification()
	c.emitEvent("fault", fmt.Sprintf("fault set: %s", code))
	// 恢复时回到之前状态
	_ = oldStatus
}

// ClearFault 清除故障
func (c *Charger) ClearFault() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.faultCode = "NoError"
	c.setStatus(Available)
	c.sendStatusNotification()
	c.emitEvent("fault", "fault cleared")
}

// Snapshot 返回当前状态快照
func (c *Charger) Snapshot() Telemetry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ms := c.meter.Snapshot()
	return Telemetry{
		Timestamp:       time.Now(),
		ConnectionState: c.connectionState,
		Status:          c.status,
		TransactionID:   c.transactionID,
		TargetCurrentA:  c.targetCurrentA,
		ActualCurrentA:  ms.ActualCurrentA,
		PowerKW:         ms.PowerKW,
		EnergyKWh:       ms.EnergyKWh,
		SOC:             c.soc,
		FaultCode:       c.faultCode,
		PhaseCount:      c.meterPhaseCount(),
		VoltageV:        c.config.VoltageV,
		MaxCurrentA:     c.config.MaxCurrentA,
		PhaseAssignment: c.config.PhaseAssignment,
		BatteryCapacityKWh: c.batteryCapacityKWh,
		TargetSOC:          c.targetSOC,
	}
}

func (c *Charger) meterPhaseCount() int {
	if c.config.Phase == "three" {
		return 3
	}
	return 1
}

func (c *Charger) setStatus(s Status) {
	old := c.status
	c.status = s
	if old != s {
		c.emitEvent("state", fmt.Sprintf("status %s -> %s", old, s))
	}
	// W-E: 挂起态归零电流/功率，避免残留假读数
	if s == SuspendedEVSE || s == SuspendedEV {
		c.meter.ZeroFlow()
		c.actualCurrentA = 0
		c.powerKW = 0
	}
}

func (c *Charger) emitStateChange(oldState, newState ConnectionState) {
	if c.onStateChange != nil {
		c.onStateChange(c.config.ID, string(oldState), string(newState))
	}
}

func (c *Charger) emitTelemetry(snap Telemetry) {
	if c.onTelemetry != nil {
		c.onTelemetry(c.config.ID, snap)
	}
}

func (c *Charger) emitEvent(typ, msg string) {
	if c.onEvent != nil {
		c.onEvent(c.config.ID, Event{Timestamp: time.Now(), Type: typ, Message: msg})
	}
}

// run 是主循环
func (c *Charger) run() {
	// 建立 WebSocket 连接
	dialer := websocket.Dialer{
		Subprotocols: []string{"ocpp1.6"},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, resp, err := dialer.Dial(c.config.Endpoint, nil)
	if err != nil {
		log.Printf("[%s] dial failed: %v", c.config.ID, err)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		c.mu.Lock()
		c.connectionState = Error
		c.emitStateChange(Connecting, Error)
		c.emitEvent("error", fmt.Sprintf("dial failed: %v", err))
		manual := c.manualStop
		c.mu.Unlock()
		if !manual {
			go c.scheduleReconnect()
		}
		return
	}

	c.mu.Lock()
	c.conn = conn
	c.connectionState = Connected
	c.reconnectDelay = 0 // 连上即重置退避
	c.emitStateChange(Connecting, Connected)
	c.emitEvent("connect", fmt.Sprintf("connected to %s", c.config.Endpoint))
	hbInterval := c.heartbeatInterval
	stopCh := c.stopCh // 捕获本连接的 stopCh(重连会替换字段,无锁读字段有 data race)
	c.mu.Unlock()

	go c.readLoop(stopCh)
	go c.writeLoop(stopCh)

	// 发送 BootNotification
	c.sendBootNotification()
	c.sendStatusNotification()

	// 自动重连续联:掉线前在充电 → 重新鉴权并补发 StartTransaction
	// (CSMS 断线时会关旧会话,不补发则出现"桩在充、CSMS 无会话"的数据失真)
	c.mu.Lock()
	if c.status == Charging && !c.startPending {
		c.startPending = true
		c.sendAuthorize()
		c.emitEvent("resume", "reconnected while charging, re-authorizing to resume transaction")
	}
	c.mu.Unlock()

	// 定时器
	hbTicker := time.NewTicker(hbInterval)
	defer hbTicker.Stop()
	meterTicker := time.NewTicker(time.Duration(c.config.MeterIntervalSec) * time.Second)
	defer meterTicker.Stop()
	updateTicker := time.NewTicker(1 * time.Second)
	defer updateTicker.Stop()

	// meterTicking: 上一 tick 是否处于充电(本连接内);进入充电首 tick 需 Touch 计时基准
	meterTicking := false

	for {
		select {
		case <-stopCh:
			return
		case <-hbTicker.C:
			c.sendHeartbeat()
		case <-meterTicker.C:
			c.mu.Lock()
			if c.status == Charging {
				c.sendMeterValues()
			}
			c.mu.Unlock()
		case <-updateTicker.C:
			c.mu.Lock()
			now := time.Now()
			batteryFull := false
			if c.status == Charging {
				// 进入充电的首个 tick:重置计时基准,避免把空闲/断线间隔计入能量
				if !meterTicking {
					c.meter.Touch(now)
					meterTicking = true
				}
				c.meter.Update(now)
				ms := c.meter.Snapshot()
				c.actualCurrentA = ms.ActualCurrentA
				c.powerKW = ms.PowerKW
				c.energyKWh = ms.EnergyKWh

				// 电池模型:SOC 由桩表能量增量物理驱动
				// soc += ΔE(kWh) × 充电效率 / 容量 × 100
				if ms.EnergyKWh < c.socEnergyBase {
					// 桩表被 ResetEnergy(新交易),重置结算基准
					c.socEnergyBase = ms.EnergyKWh
				}
				if deltaE := ms.EnergyKWh - c.socEnergyBase; deltaE > 0 {
					c.soc += deltaE * chargeEfficiency / c.batteryCapacityKWh * 100.0
					c.socEnergyBase = ms.EnergyKWh
					if c.soc > 100 {
						c.soc = 100
					}
				}

				// profile 最小实现：ramp_up 每 30 秒增加 2A
				if c.profile == "ramp_up" && !c.profileStart.IsZero() {
					elapsed := now.Sub(c.profileStart).Seconds()
					ramp := 5.0 + float64(int(elapsed)/30)*2.0
					if ramp > c.config.MaxCurrentA {
						ramp = c.config.MaxCurrentA
					}
					c.targetCurrentA = ramp
				}

				// CC-CV 锥形:SOC≥80% 后电流上限锥减(真车行为);
				// 实际下发 = min(用户/LB 目标, 锥形上限),不覆盖 targetCurrentA 意图
				eff := c.targetCurrentA
				if cap := c.taperCurrentCap(); eff > cap {
					eff = cap
				}
				c.meter.SetTargetCurrent(eff)

				// 到达目标 SOC → 自动停充(仿真真车充满,只触发一次)
				if c.soc >= c.targetSOC && !c.fullStopSent {
					c.fullStopSent = true
					batteryFull = true
				}
			} else {
				meterTicking = false
			}
			// 在锁内直接构建快照，避免调用 Snapshot() 导致的 RLock 重入死锁
			snap := Telemetry{
				Timestamp:       now,
				ConnectionState: c.connectionState,
				Status:          c.status,
				TransactionID:   c.transactionID,
				TargetCurrentA:  c.targetCurrentA,
				ActualCurrentA:  c.actualCurrentA,
				PowerKW:         c.powerKW,
				EnergyKWh:       c.energyKWh,
				SOC:             c.soc,
				FaultCode:       c.faultCode,
				PhaseCount:      c.meterPhaseCount(),
				VoltageV:        c.config.VoltageV,
				MaxCurrentA:     c.config.MaxCurrentA,
				PhaseAssignment: c.config.PhaseAssignment,
				BatteryCapacityKWh: c.batteryCapacityKWh,
				TargetSOC:          c.targetSOC,
			}
			c.mu.Unlock()
			c.emitTelemetry(snap)
			if batteryFull {
				// 锁外调用 Stop(内部自行加锁),避免重入死锁
				c.emitEvent("stop", fmt.Sprintf("battery reached target SOC %.0f%%, stopping charge", snap.TargetSOC))
				go c.Stop()
			}
		}
	}
}

func (c *Charger) readLoop(stopCh chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
		}
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			return
		}
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[%s] read error: %v", c.config.ID, err)
			}
			c.mu.Lock()
			if c.manualStop {
				// 人为断开触发的读错误:状态已由 disconnectLocked 处理,不重连
				c.mu.Unlock()
				return
			}
			c.connectionState = Error
			c.emitStateChange(Connected, Error)
			c.emitEvent("error", fmt.Sprintf("read error: %v", err))
			// 停掉本连接的 run()/writeLoop,释放旧连接,准备自动重连
			// (仅当 stopCh 仍是本连接的,避免与并发 Disconnect/重连互踩)
			if c.stopCh == stopCh {
				c.writeMu.Lock()
				if c.conn != nil {
					c.conn.Close()
					c.conn = nil
				}
				c.writeMu.Unlock()
				close(c.stopCh)
				c.stopCh = make(chan struct{})
			}
			c.mu.Unlock()
			go c.scheduleReconnect()
			return
		}
		if msgType != websocket.TextMessage {
			continue
		}
		c.handleMessage(data)
	}
}

func (c *Charger) writeLoop(stopCh chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		case msg := <-c.sendCh:
			c.writeMu.Lock()
			conn := c.conn
			if conn != nil {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					log.Printf("[%s] write error: %v", c.config.ID, err)
				}
			}
			c.writeMu.Unlock()
		}
	}
}

// sendJSON 发送 JSON 对象（经过 json.Marshal）
func (c *Charger) sendJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[%s] marshal error: %v", c.config.ID, err)
		return
	}
	c.sendRaw(data)
}

// sendRaw 发送原始字节到 sendCh
func (c *Charger) sendRaw(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		log.Printf("[%s] send channel full", c.config.ID)
	}
}

func (c *Charger) sendFrame(frame *ocpp16.Frame) {
	data, err := frame.Marshal()
	if err != nil {
		log.Printf("[%s] marshal frame error: %v", c.config.ID, err)
		return
	}
	c.sendRaw(data)
	c.emitEvent("ocpp_tx", frame.String())
}

func (c *Charger) sendCallResult(uniqueID string, payload interface{}) {
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.CallResult,
		UniqueID:      uniqueID,
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendCallError(uniqueID, code, desc string) {
	frame := &ocpp16.Frame{
		MessageTypeID:    ocpp16.CallError,
		UniqueID:         uniqueID,
		ErrorCode:        code,
		ErrorDescription: desc,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendBootNotification() {
	payload := ocpp16.BootNotificationReq{
		ChargePointModel:        "AC-Simulator-v2",
		ChargePointVendor:       "SimCorp",
		ChargePointSerialNumber: "SN-" + c.config.ID,
		FirmwareVersion:         "v0.2.0",
		MeterType:               "AC-Sim-Meter",
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      fmt.Sprintf("boot-%s-%d", c.config.ID, time.Now().Unix()),
		Action:        "BootNotification",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendStatusNotification() {
	payload := ocpp16.StatusNotificationReq{
		ConnectorID: c.config.ConnectorID,
		Status:      string(c.status),
		ErrorCode:   c.faultCode,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      fmt.Sprintf("status-%s-%d", c.config.ID, time.Now().Unix()),
		Action:        "StatusNotification",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendHeartbeat() {
	payload := ocpp16.HeartbeatReq{}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      fmt.Sprintf("hb-%s-%d", c.config.ID, time.Now().Unix()),
		Action:        "Heartbeat",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendAuthorize() {
	payload := ocpp16.AuthorizeReq{
		IDTag: c.config.IDTag,
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	msgID := fmt.Sprintf("auth-%s-%d", c.config.ID, time.Now().Unix())
	c.pendingAuthorizeMsgID = msgID
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      msgID,
		Action:        "Authorize",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendStartTransaction() {
	c.meter.ResetEnergy()
	payload := ocpp16.StartTransactionReq{
		ConnectorID: c.config.ConnectorID,
		IDTag:       c.config.IDTag,
		MeterStart:  int(c.meter.Snapshot().EnergyKWh * 1000),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	msgID := fmt.Sprintf("start-%s-%d", c.config.ID, time.Now().Unix())
	c.pendingStartMsgID = msgID
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      msgID,
		Action:        "StartTransaction",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendStopTransaction() {
	payload := ocpp16.StopTransactionReq{
		TransactionID: c.transactionID,
		IDTag:         c.config.IDTag,
		MeterStop:     int(c.meter.Snapshot().EnergyKWh * 1000),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      fmt.Sprintf("stop-%s-%d", c.config.ID, time.Now().Unix()),
		Action:        "StopTransaction",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) sendMeterValues() {
	ms := c.meter.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)

	sampledValues := []ocpp16.SampledValue{}

	// Voltage per phase
	for i := 1; i <= c.meterPhaseCount(); i++ {
		sampledValues = append(sampledValues, ocpp16.SampledValue{
			Value:     fmt.Sprintf("%.1f", c.config.VoltageV),
			Context:   "Sample.Periodic",
			Format:    "Raw",
			Measurand: "Voltage",
			Phase:     fmt.Sprintf("L%d-N", i),
			Location:  "Outlet",
			Unit:      "V",
		})
	}

	// Current per phase
	for i := 1; i <= c.meterPhaseCount(); i++ {
		var current float64
		switch i {
		case 1:
			current = ms.CurrentL1
		case 2:
			current = ms.CurrentL2
		case 3:
			current = ms.CurrentL3
		}
		sampledValues = append(sampledValues, ocpp16.SampledValue{
			Value:     fmt.Sprintf("%.3f", current),
			Context:   "Sample.Periodic",
			Format:    "Raw",
			Measurand: "Current.Import",
			Phase:     fmt.Sprintf("L%d", i),
			Location:  "Outlet",
			Unit:      "A",
		})
	}

	// Power
	power := ms.PowerKW * 1000
	sampledValues = append(sampledValues, ocpp16.SampledValue{
		Value:     fmt.Sprintf("%.3f", power),
		Context:   "Sample.Periodic",
		Format:    "Raw",
		Measurand: "Power.Active.Import",
		Location:  "Outlet",
		Unit:      "W",
	})

	// Energy
	sampledValues = append(sampledValues, ocpp16.SampledValue{
		Value:     fmt.Sprintf("%.3f", ms.EnergyKWh),
		Context:   "Sample.Periodic",
		Format:    "Raw",
		Measurand: "Energy.Active.Import.Register",
		Location:  "Outlet",
		Unit:      "kWh",
	})

	// SOC
	sampledValues = append(sampledValues, ocpp16.SampledValue{
		Value:     fmt.Sprintf("%.1f", c.soc),
		Context:   "Sample.Periodic",
		Format:    "Raw",
		Measurand: "SoC",
		Unit:      "Percent",
	})

	payload := ocpp16.MeterValuesReq{
		ConnectorID:   c.config.ConnectorID,
		TransactionID: c.transactionID,
		MeterValue: []ocpp16.MeterValue{
			{
				Timestamp:    now,
				SampledValue: sampledValues,
			},
		},
	}
	payloadBytes, _ := ocpp16.BuildPayload(payload)
	frame := &ocpp16.Frame{
		MessageTypeID: ocpp16.Call,
		UniqueID:      fmt.Sprintf("meter-%s-%d", c.config.ID, time.Now().Unix()),
		Action:        "MeterValues",
		Payload:       payloadBytes,
	}
	c.sendFrame(frame)
}

func (c *Charger) handleMessage(data []byte) {
	frame, err := ocpp16.Unmarshal(data)
	if err != nil {
		c.emitEvent("error", fmt.Sprintf("unmarshal error: %v", err))
		return
	}
	c.emitEvent("ocpp_rx", frame.String())

	switch frame.MessageTypeID {
	case ocpp16.CallResult:
		c.handleCallResult(frame)
	case ocpp16.CallError:
		c.emitEvent("error", fmt.Sprintf("callerror: %s %s", frame.ErrorCode, frame.ErrorDescription))
	case ocpp16.Call:
		c.handleServerCall(frame)
	}
}

func (c *Charger) handleCallResult(frame *ocpp16.Frame) {
	// BootNotification
	var bootConf ocpp16.BootNotificationConf
	if err := ocpp16.ParsePayload(frame.Payload, &bootConf); err == nil && bootConf.Status == "Accepted" {
		c.mu.Lock()
		if bootConf.Interval > 0 {
			c.heartbeatInterval = time.Duration(bootConf.Interval) * time.Second
		}
		c.mu.Unlock()
		c.emitEvent("boot", fmt.Sprintf("BootNotification accepted, interval=%ds", bootConf.Interval))
		return
	}

	// Authorize
	c.mu.Lock()
	isAuth := frame.UniqueID == c.pendingAuthorizeMsgID
	if isAuth {
		c.pendingAuthorizeMsgID = ""
	}
	c.mu.Unlock()
	if isAuth {
		var authConf ocpp16.AuthorizeConf
		if err := ocpp16.ParsePayload(frame.Payload, &authConf); err == nil && authConf.IDTagInfo.Status == "Accepted" {
			c.mu.Lock()
			c.sendStartTransaction()
			c.mu.Unlock()
			c.emitEvent("start", "Authorize accepted, StartTransaction sent")
		} else {
			c.mu.Lock()
			c.startPending = false
			c.setStatus(Available)
			c.sendStatusNotification()
			c.mu.Unlock()
			c.emitEvent("start", "Authorize rejected, transaction cancelled")
		}
		return
	}

	// StartTransaction
	c.mu.Lock()
	isStart := frame.UniqueID == c.pendingStartMsgID
	if isStart {
		c.pendingStartMsgID = ""
	}
	c.mu.Unlock()
	if isStart {
		var startConf ocpp16.StartTransactionConf
		if err := ocpp16.ParsePayload(frame.Payload, &startConf); err == nil && startConf.IDTagInfo.Status == "Accepted" {
			c.mu.Lock()
			c.transactionID = startConf.TransactionID
			c.startPending = false
			c.setStatus(Charging)
			c.sendStatusNotification()
			c.mu.Unlock()
			c.emitEvent("start", fmt.Sprintf("StartTransaction accepted, txID=%d", c.transactionID))
		} else {
			c.mu.Lock()
			c.startPending = false
			c.setStatus(Available)
			c.sendStatusNotification()
			c.mu.Unlock()
			c.emitEvent("start", "StartTransaction rejected, transaction cancelled")
		}
		return
	}
}

func (c *Charger) handleServerCall(frame *ocpp16.Frame) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch frame.Action {
	case "SetChargingProfile":
		var req ocpp16.SetChargingProfileReq
		if err := ocpp16.ParsePayload(frame.Payload, &req); err != nil {
			c.sendCallError(frame.UniqueID, "FormationViolation", err.Error())
			return
		}
		schedule := req.CSChargingProfiles.ChargingSchedule
		if len(schedule.ChargingSchedulePeriod) == 0 {
			c.sendCallResult(frame.UniqueID, ocpp16.SetChargingProfileConf{Status: "Rejected"})
			return
		}
		period := schedule.ChargingSchedulePeriod[0]
		limit := period.Limit

		if schedule.ChargingRateUnit == "W" {
			var currentA float64
			if c.config.Phase == "single" {
				currentA = limit / (c.config.VoltageV * c.config.PowerFactor)
			} else {
				currentA = limit / (1.732 * c.config.VoltageV * c.config.PowerFactor)
			}
			limit = currentA
		}
		if limit > c.config.MaxCurrentA {
			limit = c.config.MaxCurrentA
		}
		if limit < 6.0 {
			c.targetCurrentA = 0
			c.meter.SetTargetCurrent(0)
			c.setStatus(SuspendedEVSE)
			c.sendStatusNotification()
			c.emitEvent("profile", fmt.Sprintf("SetChargingProfile limit %.1fA below 6A, suspended", limit))
		} else {
			c.targetCurrentA = limit
			c.meter.SetTargetCurrent(limit)
			if c.status == SuspendedEVSE {
				c.setStatus(Charging)
				c.sendStatusNotification()
			}
			c.emitEvent("profile", fmt.Sprintf("SetChargingProfile limit %.1fA", limit))
		}
		c.sendCallResult(frame.UniqueID, ocpp16.SetChargingProfileConf{Status: "Accepted"})

	case "ClearChargingProfile":
		c.targetCurrentA = c.config.MaxCurrentA
		c.meter.SetTargetCurrent(c.config.MaxCurrentA)
		if c.status == SuspendedEVSE {
			c.setStatus(Charging)
			c.sendStatusNotification()
		}
		c.emitEvent("profile", "ClearChargingProfile, restored to max current")
		c.sendCallResult(frame.UniqueID, ocpp16.ClearChargingProfileConf{Status: "Accepted"})

	case "RemoteStartTransaction":
		var req ocpp16.RemoteStartTransactionReq
		ocpp16.ParsePayload(frame.Payload, &req)
		go func() {
			// 延迟一下，避免锁冲突
			time.Sleep(100 * time.Millisecond)
			c.Start()
		}()
		c.sendCallResult(frame.UniqueID, ocpp16.RemoteStartTransactionConf{Status: "Accepted"})
		c.emitEvent("remote", "RemoteStartTransaction accepted")

	case "RemoteStopTransaction":
		go func() {
			time.Sleep(100 * time.Millisecond)
			c.Stop()
		}()
		c.sendCallResult(frame.UniqueID, ocpp16.RemoteStopTransactionConf{Status: "Accepted"})
		c.emitEvent("remote", "RemoteStopTransaction accepted")

	case "Reset":
		c.sendCallResult(frame.UniqueID, ocpp16.ResetConf{Status: "Accepted"})
		c.emitEvent("remote", "Reset accepted")
		go func() {
			time.Sleep(500 * time.Millisecond)
			c.Disconnect()
			time.Sleep(500 * time.Millisecond)
			c.Connect()
		}()

	case "ChangeAvailability":
		var req ocpp16.ChangeAvailabilityReq
		ocpp16.ParsePayload(frame.Payload, &req)
		if req.Type == "Inoperative" {
			c.setStatus(Unavailable)
		} else {
			c.setStatus(Available)
		}
		c.sendStatusNotification()
		c.sendCallResult(frame.UniqueID, ocpp16.ChangeAvailabilityConf{Status: "Accepted"})
		c.emitEvent("remote", fmt.Sprintf("ChangeAvailability to %s", req.Type))

	default:
		c.sendCallResult(frame.UniqueID, map[string]interface{}{"status": "NotSupported"})
		c.emitEvent("ocpp_rx", fmt.Sprintf("unsupported action: %s", frame.Action))
	}
}

// IsConnected 是否已连接
func (c *Charger) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connectionState == Connected
}

// IsCharging 是否正在充电
func (c *Charger) IsCharging() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status == Charging
}

// Status 返回当前状态
func (c *Charger) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// ConnectionState 返回连接状态
func (c *Charger) ConnectionState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connectionState
}

// TransactionID 返回交易 ID
func (c *Charger) TransactionID() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transactionID
}

// FaultCode 返回故障码
func (c *Charger) FaultCode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.faultCode
}

// TargetCurrentA 返回目标电流
func (c *Charger) TargetCurrentA() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.targetCurrentA
}

// ActualCurrentA 返回实际电流
func (c *Charger) ActualCurrentA() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.actualCurrentA
}

// PowerKW 返回功率
func (c *Charger) PowerKW() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.powerKW
}

// EnergyKWh 返回累计电量
func (c *Charger) EnergyKWh() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.energyKWh
}

// SOC 返回 SOC
func (c *Charger) SOC() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.soc
}

// SetProfile 设置测试 profile
func (c *Charger) SetProfile(profile string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profile = profile
	c.profileStart = time.Now()
	c.emitEvent("profile", fmt.Sprintf("profile switched to %s", profile))
}

// ResetEndpoint 修改 OCPP endpoint
func (c *Charger) ResetEndpoint(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Endpoint = endpoint
	c.emitEvent("config", fmt.Sprintf("endpoint changed to %s", endpoint))
}
