package webconsole

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ljd166/ac-charger-simulator/internal/charger"
	"github.com/ljd166/ac-charger-simulator/internal/config"
	"github.com/ljd166/ac-charger-simulator/internal/telemetry"
)

type mockManager struct {
	chargers map[string]*charger.Charger
	endpoint string
}

func (m *mockManager) GetCharger(id string) (*charger.Charger, bool) {
	c, ok := m.chargers[id]
	return c, ok
}

func (m *mockManager) AllChargers() []*charger.Charger {
	result := make([]*charger.Charger, 0, len(m.chargers))
	for _, c := range m.chargers {
		result = append(result, c)
	}
	return result
}

func (m *mockManager) SetEndpoint(endpoint string) {
	m.endpoint = endpoint
}

func (m *mockManager) GetEndpoint() string {
	return m.endpoint
}

func newTestServer() (*Server, *mockManager, *telemetry.Hub) {
	cfg := config.ChargerConfig{
		ID:              "TEST-001",
		ConnectorID:     1,
		Endpoint:        "ws://127.0.0.1:9999/ocpp/TEST-001",
		IDTag:           "TEST-CARD",
		Phase:           "single",
		PhaseAssignment: "L1",
		MaxCurrentA:     32,
		VoltageV:        230,
		PowerFactor:     0.98,
		MeterIntervalSec: 5,
	}
	c := charger.NewCharger(cfg)
	mgr := &mockManager{
		chargers: map[string]*charger.Charger{c.ID(): c},
		endpoint: "ws://127.0.0.1:9000",
	}
	hub := telemetry.NewHub()
	c.SetCallbacks(
		hub.OnStateChange,
		hub.OnTelemetry,
		hub.OnEvent,
	)
	server := NewServer("127.0.0.1:0", mgr, hub)
	return server, mgr, hub
}

func TestHandleState(t *testing.T) {
	server, _, _ := newTestServer()
	req := httptest.NewRequest("GET", "/api/state", nil)
	w := httptest.NewRecorder()
	server.handleState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "global") {
		t.Fatal("expected response to contain global state")
	}
}

func TestHandleSetEndpoint(t *testing.T) {
	server, mgr, _ := newTestServer()
	body := bytes.NewBufferString(`{"endpoint":"ws://new:9000"}`)
	req := httptest.NewRequest("POST", "/api/config/ocpp-endpoint", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleSetEndpoint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if mgr.GetEndpoint() != "ws://new:9000" {
		t.Fatalf("expected endpoint ws://new:9000, got %s", mgr.GetEndpoint())
	}
}

func TestHandleTargetCurrent(t *testing.T) {
	server, mgr, _ := newTestServer()
	body := bytes.NewBufferString(`{"current_a":16}`)
	req := httptest.NewRequest("POST", "/api/chargers/TEST-001/target-current", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	c, _ := mgr.GetCharger("TEST-001")
	if c.TargetCurrentA() != 16 {
		t.Fatalf("expected target current 16A, got %f", c.TargetCurrentA())
	}
}

func TestHandleFault(t *testing.T) {
	server, mgr, _ := newTestServer()
	body := bytes.NewBufferString(`{"code":"EarthFailure"}`)
	req := httptest.NewRequest("POST", "/api/chargers/TEST-001/fault", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	c, _ := mgr.GetCharger("TEST-001")
	if c.Status() != charger.Faulted {
		t.Fatalf("expected status Faulted, got %s", c.Status())
	}
}

func TestHandleChargerNotFound(t *testing.T) {
	server, _, _ := newTestServer()
	req := httptest.NewRequest("POST", "/api/chargers/UNKNOWN/start", nil)
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestHandleStateWithoutTelemetry(t *testing.T) {
	cfg1 := config.ChargerConfig{ID: "SIM-001", ConnectorID: 1, Endpoint: "ws://127.0.0.1:9000/ocpp/1", Phase: "single", MaxCurrentA: 32}
	cfg2 := config.ChargerConfig{ID: "SIM-002", ConnectorID: 1, Endpoint: "ws://127.0.0.1:9000/ocpp/2", Phase: "three", MaxCurrentA: 16}
	c1 := charger.NewCharger(cfg1)
	c2 := charger.NewCharger(cfg2)
	mgr := &mockManager{
		chargers: map[string]*charger.Charger{c1.ID(): c1, c2.ID(): c2},
		endpoint: "ws://127.0.0.1:9000",
	}
	hub := telemetry.NewHub()
	// 注意：不注册 callbacks，所以 hub 中不会有任何 telemetry
	server := NewServer("127.0.0.1:0", mgr, hub)

	req := httptest.NewRequest("GET", "/api/state", nil)
	w := httptest.NewRecorder()
	server.handleState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	var resp response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", resp.Data)
	}
	global, ok := data["global"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected global map, got %T", data["global"])
	}
	if global["configured_count"] != float64(2) {
		t.Fatalf("expected configured_count 2, got %v", global["configured_count"])
	}
	chargers, ok := data["chargers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected chargers map, got %T", data["chargers"])
	}
	if len(chargers) != 2 {
		t.Fatalf("expected 2 chargers, got %d", len(chargers))
	}
	if chargers["SIM-001"] == nil {
		t.Fatal("expected SIM-001 in chargers")
	}
	if chargers["SIM-002"] == nil {
		t.Fatal("expected SIM-002 in chargers")
	}
	// 验证每个 charger 有可渲染字段
	sim001, ok := chargers["SIM-001"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected SIM-001 map, got %T", chargers["SIM-001"])
	}
	if sim001["charger_status"] == nil {
		t.Fatal("expected charger_status in SIM-001")
	}
	if sim001["ocpp_connection_state"] == nil {
		t.Fatal("expected ocpp_connection_state in SIM-001")
	}
}

func TestStartPortInUse(t *testing.T) {
	// 先占用一个随机端口
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	addr := l.Addr().String()
	mgr := &mockManager{chargers: map[string]*charger.Charger{}}
	hub := telemetry.NewHub()
	server := NewServer(addr, mgr, hub)

	if err := server.Start(); err == nil {
		t.Fatal("expected error when port is in use")
	}
}
