package webconsole

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/ljd166/ac-charger-simulator/internal/charger"
	"github.com/ljd166/ac-charger-simulator/internal/telemetry"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Manager 提供管理桩的能力
type Manager interface {
	GetCharger(id string) (*charger.Charger, bool)
	AllChargers() []*charger.Charger
	SetEndpoint(endpoint string)
	GetEndpoint() string
}

// Server Web Console 服务器
type Server struct {
	addr    string
	mgr     Manager
	hub     *telemetry.Hub
	server  *http.Server
}

// NewServer 创建新服务器
func NewServer(addr string, mgr Manager, hub *telemetry.Hub) *Server {
	s := &Server{
		addr: addr,
		mgr:  mgr,
		hub:  hub,
	}
	r := mux.NewRouter()
	
	// API 路由
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/state", s.handleState).Methods("GET")
	api.HandleFunc("/config/ocpp-endpoint", s.handleSetEndpoint).Methods("POST")
	api.HandleFunc("/chargers/{id}/connect", s.handleConnect).Methods("POST")
	api.HandleFunc("/chargers/{id}/disconnect", s.handleDisconnect).Methods("POST")
	api.HandleFunc("/chargers/{id}/start", s.handleStart).Methods("POST")
	api.HandleFunc("/chargers/{id}/stop", s.handleStop).Methods("POST")
	api.HandleFunc("/chargers/{id}/target-current", s.handleTargetCurrent).Methods("POST")
	api.HandleFunc("/chargers/{id}/fault", s.handleFault).Methods("POST")
	api.HandleFunc("/chargers/{id}/profile", s.handleProfile).Methods("POST")
	api.HandleFunc("/chargers/{id}/history", s.handleHistory).Methods("GET")
	api.HandleFunc("/chargers/{id}/soc", s.handleSetSOC).Methods("POST")
	api.HandleFunc("/chargers/all/start", s.handleAllStart).Methods("POST")
	api.HandleFunc("/chargers/all/stop", s.handleAllStop).Methods("POST")
	
	// WebSocket
	r.HandleFunc("/ws/telemetry", s.handleWebSocket)
	
	// 静态文件
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("web/static")))
	
	s.server = &http.Server{
		Addr:    addr,
		Handler: r,
	}
	return s
}

// Start 启动服务器
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	log.Printf("[WebConsole] starting on http://%s", s.addr)
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[WebConsole] server error: %v", err)
		}
	}()
	return nil
}

// Stop 停止服务器
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		log.Printf("[WebConsole] shutdown error: %v", err)
	}
}

type response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	chargers := s.mgr.AllChargers()
	connected := 0
	charging := 0
	for _, c := range chargers {
		if c.IsConnected() {
			connected++
		}
		if c.IsCharging() {
			charging++
		}
	}
	
	state := s.hub.GlobalState(s.mgr.GetEndpoint(), len(chargers), connected, charging)
	
	// 优先取 hub telemetry，没有则 fallback 到 Snapshot()
	latest := s.hub.Latest()
	chargerData := make(map[string]interface{}, len(chargers))
	for _, c := range chargers {
		id := c.ID()
		if snap, ok := latest[id]; ok {
			chargerData[id] = snap
		} else {
			chargerData[id] = c.Snapshot()
		}
	}
	
	writeJSON(w, http.StatusOK, response{
		Success: true,
		Data: map[string]interface{}{
			"global":   state,
			"chargers": chargerData,
			"events":   s.hub.Events(50),
		},
	})
}

func (s *Server) handleSetEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	s.mgr.SetEndpoint(req.Endpoint)
	writeJSON(w, http.StatusOK, response{Success: true, Message: "endpoint updated"})
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	if err := c.Connect(); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "connecting"})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	c.Disconnect()
	writeJSON(w, http.StatusOK, response{Success: true, Message: "disconnected"})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	if err := c.Start(); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "started"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	if err := c.Stop(); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "stopped"})
}

func (s *Server) handleTargetCurrent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	var req struct {
		CurrentA float64 `json:"current_a"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	if err := c.SetTargetCurrent(req.CurrentA); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "target current set"})
}

func (s *Server) handleFault(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	if req.Code == "" || req.Code == "NoError" || req.Code == "Clear" {
		c.ClearFault()
	} else {
		c.SetFault(req.Code)
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "fault status updated"})
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	var req struct {
		Profile string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	c.SetProfile(req.Profile)
	writeJSON(w, http.StatusOK, response{Success: true, Message: "profile set"})
}

// handleSetSOC 直接设定电池 SOC(测试复位用)
func (s *Server) handleSetSOC(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	c, ok := s.mgr.GetCharger(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	var req struct {
		SOC float64 `json:"soc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{Success: false, Message: err.Error()})
		return
	}
	c.SetSOC(req.SOC)
	writeJSON(w, http.StatusOK, response{Success: true, Message: "soc set"})
}

// handleHistory 返回单桩近期遥测历史(图表回填用)
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	if _, ok := s.mgr.GetCharger(id); !ok {
		writeJSON(w, http.StatusNotFound, response{Success: false, Message: "charger not found"})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Data: s.hub.History(id)})
}

func (s *Server) handleAllStart(w http.ResponseWriter, r *http.Request) {
	for _, c := range s.mgr.AllChargers() {
		_ = c.Start()
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "all started"})
}

func (s *Server) handleAllStop(w http.ResponseWriter, r *http.Request) {
	for _, c := range s.mgr.AllChargers() {
		_ = c.Stop()
	}
	writeJSON(w, http.StatusOK, response{Success: true, Message: "all stopped"})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] upgrade error: %v", err)
		return
	}
	defer conn.Close()

	clientID := fmt.Sprintf("ws-%d", time.Now().UnixNano())
	ch := s.hub.Subscribe(clientID)
	defer s.hub.Unsubscribe(clientID)

	// 发送当前状态
	state := s.hub.GlobalState(s.mgr.GetEndpoint(), len(s.mgr.AllChargers()), 0, 0)
	chargers := s.mgr.AllChargers()
	connected := 0
	charging := 0
	for _, c := range chargers {
		if c.IsConnected() {
			connected++
		}
		if c.IsCharging() {
			charging++
		}
	}
	state.ConnectedCount = connected
	state.ChargingCount = charging
	
	msg := telemetry.Broadcast{
		Type:  "state",
		State: state,
	}
	conn.WriteJSON(msg)

	// 发送最新遥测
	for _, snap := range s.hub.Latest() {
		conn.WriteJSON(telemetry.Broadcast{Type: "telemetry", Telemetry: snap})
	}

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		case <-time.After(30 * time.Second):
			if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
				return
			}
		}
	}
}
