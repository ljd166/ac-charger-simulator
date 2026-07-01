package webconsole

import (
	"bytes"
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
	server, _, _ := newTestServer()
	body := bytes.NewBufferString(`{"current_a":16}`)
	req := httptest.NewRequest("POST", "/api/chargers/TEST-001/target-current", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d", w.Code)
	}
}

func TestHandleFault(t *testing.T) {
	server, _, _ := newTestServer()
	body := bytes.NewBufferString(`{"code":"EarthFailure"}`)
	req := httptest.NewRequest("POST", "/api/chargers/TEST-001/fault", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
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
