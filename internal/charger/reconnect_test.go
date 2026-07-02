package charger

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ljd166/ac-charger-simulator/internal/config"
)

// startWSServer 在指定地址起一个最小 OCPP CSMS,返回关闭函数;bootCount 记录收到的 BootNotification 数。
// 注意跟踪已升级的 websocket 连接:http.Server.Close 不会关 hijacked 连接,必须显式关。
func startWSServer(t *testing.T, addr string, bootCount *atomic.Int32) func() {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	var connMu sync.Mutex
	var conns []*websocket.Conn
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connMu.Lock()
		conns = append(conns, conn)
		connMu.Unlock()
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			var raw []interface{}
			json.Unmarshal(data, &raw)
			if len(raw) >= 4 {
				action, _ := raw[2].(string)
				msgID, _ := raw[1].(string)
				if action == "BootNotification" {
					bootCount.Add(1)
				}
				conn.WriteJSON([]interface{}{3, msgID, map[string]interface{}{"status": "Accepted"}})
			}
		}
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return func() {
		srv.Close()
		ln.Close()
		connMu.Lock()
		for _, cn := range conns {
			cn.Close()
		}
		connMu.Unlock()
	}
}

// TestAutoReconnect_AfterServerRestart CSMS 重启后模拟桩应自动重连(不再永久卡 error)
func TestAutoReconnect_AfterServerRestart(t *testing.T) {
	// 固定端口,保证重启后地址不变
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	var boots atomic.Int32
	stop1 := startWSServer(t, addr, &boots)

	c := NewCharger(config.ChargerConfig{
		ID: "TEST-RC", ConnectorID: 1,
		Endpoint: "ws://" + addr + "/ocpp/TEST-RC",
		Phase:    "single", MaxCurrentA: 32,
	})
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return boots.Load() >= 1 }, "first boot")

	// CSMS 掉线
	stop1()
	waitFor(t, 3*time.Second, func() bool {
		return c.Snapshot().ConnectionState == Error || c.Snapshot().ConnectionState == Connecting
	}, "detect disconnect")

	// CSMS 恢复(同地址)→ 应在退避(首轮5s)内自动重连并重发 Boot
	stop2 := startWSServer(t, addr, &boots)
	defer stop2()
	waitFor(t, 15*time.Second, func() bool { return boots.Load() >= 2 }, "auto reconnect boot")
	waitFor(t, 3*time.Second, func() bool { return c.Snapshot().ConnectionState == Connected }, "state connected")
	c.Disconnect()
}

// TestAutoReconnect_ManualDisconnectSuppressed 人为断开不得自动重连
func TestAutoReconnect_ManualDisconnectSuppressed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	var boots atomic.Int32
	stop := startWSServer(t, addr, &boots)
	defer stop()

	c := NewCharger(config.ChargerConfig{
		ID: "TEST-MD", ConnectorID: 1,
		Endpoint: "ws://" + addr + "/ocpp/TEST-MD",
		Phase:    "single", MaxCurrentA: 32,
	})
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return boots.Load() >= 1 }, "first boot")

	c.Disconnect()
	time.Sleep(7 * time.Second) // 超过首轮退避 5s
	if n := boots.Load(); n != 1 {
		t.Fatalf("manual disconnect must not auto-reconnect, boots=%d", n)
	}
	if st := c.Snapshot().ConnectionState; st != Disconnected {
		t.Fatalf("expected Disconnected, got %s", st)
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}
