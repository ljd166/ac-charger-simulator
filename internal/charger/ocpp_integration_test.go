package charger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ljd166/ac-charger-simulator/internal/config"
)

// 创建一个本地 WebSocket 测试服务器，捕获 OCPP 出站消息
func newTestWSServer(t *testing.T, handler func(conn *websocket.Conn, msg []byte)) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage {
				handler(conn, data)
			}
		}
	}))
	return server
}

func TestBootNotificationFrameFormat(t *testing.T) {
	var captured []byte
	var once sync.Once
	done := make(chan struct{})
	server := newTestWSServer(t, func(conn *websocket.Conn, msg []byte) {
		var raw []interface{}
		json.Unmarshal(msg, &raw)
		if len(raw) >= 4 {
			action, _ := raw[2].(string)
			msgID, _ := raw[1].(string)
			switch action {
			case "BootNotification":
				captured = msg
				resp := []interface{}{3, msgID, map[string]interface{}{"status": "Accepted", "currentTime": time.Now().UTC().Format(time.RFC3339), "interval": 60}}
				conn.WriteJSON(resp)
				once.Do(func() { close(done) })
			case "StatusNotification":
				resp := []interface{}{3, msgID, map[string]interface{}{}}
				conn.WriteJSON(resp)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ocpp/TEST-001"
	cfg := config.ChargerConfig{
		ID: "TEST-001", ConnectorID: 1, Endpoint: wsURL,
		Phase: "single", MaxCurrentA: 32,
	}
	c := NewCharger(cfg)
	c.Connect()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for BootNotification")
	}

	// 验证是 JSON array
	var raw []interface{}
	if err := json.Unmarshal(captured, &raw); err != nil {
		t.Fatalf("captured is not valid JSON: %v", err)
	}
	if len(raw) != 4 {
		t.Fatalf("expected 4 elements, got %d: %s", len(raw), string(captured))
	}
	msgType, ok := raw[0].(float64)
	if !ok || msgType != 2 {
		t.Fatalf("expected message type 2 (Call), got %v", raw[0])
	}
	action, ok := raw[2].(string)
	if !ok || action != "BootNotification" {
		t.Fatalf("expected action BootNotification, got %v", raw[2])
	}
	payload, ok := raw[3].(map[string]interface{})
	if !ok {
		t.Fatalf("expected payload object, got %T", raw[3])
	}
	if payload["chargePointModel"] == nil {
		t.Fatal("expected chargePointModel in payload")
	}
}

func TestAuthorizeBeforeStartTransaction(t *testing.T) {
	var messages [][]byte
	var mu sync.Mutex
	server := newTestWSServer(t, func(conn *websocket.Conn, msg []byte) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
		var raw []interface{}
		json.Unmarshal(msg, &raw)
		if len(raw) >= 4 {
			action, _ := raw[2].(string)
			msgID, _ := raw[1].(string)
			switch action {
			case "BootNotification":
				resp := []interface{}{3, msgID, map[string]interface{}{"status": "Accepted", "currentTime": time.Now().UTC().Format(time.RFC3339), "interval": 60}}
				conn.WriteJSON(resp)
			case "StatusNotification":
				resp := []interface{}{3, msgID, map[string]interface{}{}}
				conn.WriteJSON(resp)
			case "Authorize":
				resp := []interface{}{3, msgID, map[string]interface{}{"idTagInfo": map[string]interface{}{"status": "Accepted"}}}
				conn.WriteJSON(resp)
			case "StartTransaction":
				resp := []interface{}{3, msgID, map[string]interface{}{"transactionId": 12345, "idTagInfo": map[string]interface{}{"status": "Accepted"}}}
				conn.WriteJSON(resp)
			case "Heartbeat":
				resp := []interface{}{3, msgID, map[string]interface{}{"currentTime": time.Now().UTC().Format(time.RFC3339)}}
				conn.WriteJSON(resp)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ocpp/TEST-002"
	cfg := config.ChargerConfig{
		ID: "TEST-002", ConnectorID: 1, Endpoint: wsURL,
		Phase: "single", MaxCurrentA: 32,
	}
	c := NewCharger(cfg)
	c.Connect()

	// 等待 Boot + Status 完成
	time.Sleep(500 * time.Millisecond)

	// 发送 Start
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 等待整个流程完成（进入 Charging 状态）
	for i := 0; i < 50; i++ {
		if c.Status() == Charging {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if c.Status() != Charging {
		t.Fatalf("expected Charging, got %s", c.Status())
	}

	// 检查消息顺序
	mu.Lock()
	var actions []string
	for _, msg := range messages {
		var raw []interface{}
		json.Unmarshal(msg, &raw)
		if len(raw) >= 4 {
			action, _ := raw[2].(string)
			actions = append(actions, action)
		}
	}
	mu.Unlock()

	authIdx := -1
	startIdx := -1
	for i, a := range actions {
		if a == "Authorize" && authIdx == -1 {
			authIdx = i
		}
		if a == "StartTransaction" && startIdx == -1 {
			startIdx = i
		}
	}
	if authIdx == -1 {
		t.Fatal("Authorize not found in messages")
	}
	if startIdx == -1 {
		t.Fatal("StartTransaction not found in messages")
	}
	if authIdx >= startIdx {
		t.Fatalf("Authorize (idx %d) must be before StartTransaction (idx %d)", authIdx, startIdx)
	}
}

func TestTransactionIdFromCSMS(t *testing.T) {
	server := newTestWSServer(t, func(conn *websocket.Conn, msg []byte) {
		var raw []interface{}
		json.Unmarshal(msg, &raw)
		if len(raw) >= 4 {
			action, _ := raw[2].(string)
			msgID, _ := raw[1].(string)
			switch action {
			case "BootNotification":
				resp := []interface{}{3, msgID, map[string]interface{}{"status": "Accepted", "currentTime": time.Now().UTC().Format(time.RFC3339), "interval": 60}}
				conn.WriteJSON(resp)
			case "StatusNotification":
				resp := []interface{}{3, msgID, map[string]interface{}{}}
				conn.WriteJSON(resp)
			case "Authorize":
				resp := []interface{}{3, msgID, map[string]interface{}{"idTagInfo": map[string]interface{}{"status": "Accepted"}}}
				conn.WriteJSON(resp)
			case "StartTransaction":
				resp := []interface{}{3, msgID, map[string]interface{}{"transactionId": 12345, "idTagInfo": map[string]interface{}{"status": "Accepted"}}}
				conn.WriteJSON(resp)
			case "Heartbeat":
				resp := []interface{}{3, msgID, map[string]interface{}{"currentTime": time.Now().UTC().Format(time.RFC3339)}}
				conn.WriteJSON(resp)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ocpp/TEST-003"
	cfg := config.ChargerConfig{
		ID: "TEST-003", ConnectorID: 1, Endpoint: wsURL,
		Phase: "single", MaxCurrentA: 32,
	}
	c := NewCharger(cfg)
	c.Connect()

	// 等待 Boot + Status
	time.Sleep(500 * time.Millisecond)

	// Start
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 等待流程完成
	for i := 0; i < 50; i++ {
		if c.Status() == Charging {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if c.Status() != Charging {
		t.Fatalf("expected Charging, got %s", c.Status())
	}

	// 检查 transaction ID
	if c.TransactionID() != 12345 {
		t.Fatalf("expected transaction ID 12345, got %d", c.TransactionID())
	}
}

func TestStartReentryRejected(t *testing.T) {
	cfg := config.ChargerConfig{
		ID: "TEST-004", ConnectorID: 1, Endpoint: "ws://127.0.0.1:9999/ocpp/TEST-004",
		Phase: "single", MaxCurrentA: 32,
	}
	c := NewCharger(cfg)
	c.startPending = true // 模拟正在 pending

	// 连接状态不是 Connected，Start 会失败
	if err := c.Start(); err == nil {
		t.Fatal("expected error when not connected")
	}

	// 即使连接了，startPending=true 也应该拒绝
	c.connectionState = Connected
	if err := c.Start(); err == nil {
		t.Fatal("expected error when start already pending")
	}
	if err := c.Start(); err == nil || !strings.Contains(err.Error(), "already pending") {
		t.Fatalf("expected 'already pending' error, got: %v", err)
	}
}
