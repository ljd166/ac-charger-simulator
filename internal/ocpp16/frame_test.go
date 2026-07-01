package ocpp16

import (
	"encoding/json"
	"testing"
)

func TestMarshalUnmarshalCall(t *testing.T) {
	nowFunc = func() int64 { return 42 }
	defer func() { nowFunc = func() int64 { return 0 } }()

	payload := map[string]interface{}{
		"chargePointModel": "TeslaWallConnector",
		"chargePointVendor": "Tesla",
	}
	payloadBytes, _ := json.Marshal(payload)

	f := &Frame{
		MessageTypeID: Call,
		UniqueID:      "boot-42",
		Action:        "BootNotification",
		Payload:       payloadBytes,
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MessageTypeID != Call {
		t.Fatalf("expected message type %d, got %d", Call, decoded.MessageTypeID)
	}
	if decoded.UniqueID != "boot-42" {
		t.Fatalf("expected unique id boot-42, got %s", decoded.UniqueID)
	}
	if decoded.Action != "BootNotification" {
		t.Fatalf("expected action BootNotification, got %s", decoded.Action)
	}
}

func TestMarshalUnmarshalCallResult(t *testing.T) {
	payload := map[string]interface{}{
		"status": "Accepted",
	}
	payloadBytes, _ := json.Marshal(payload)

	f := &Frame{
		MessageTypeID: CallResult,
		UniqueID:      "boot-42",
		Payload:       payloadBytes,
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MessageTypeID != CallResult {
		t.Fatalf("expected message type %d, got %d", CallResult, decoded.MessageTypeID)
	}
	if decoded.UniqueID != "boot-42" {
		t.Fatalf("expected unique id boot-42, got %s", decoded.UniqueID)
	}
}

func TestMarshalUnmarshalCallError(t *testing.T) {
	f := &Frame{
		MessageTypeID:    CallError,
		UniqueID:         "boot-42",
		ErrorCode:        "NotSupported",
		ErrorDescription: "Action not supported",
		ErrorDetails:     map[string]interface{}{"detail": "test"},
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MessageTypeID != CallError {
		t.Fatalf("expected message type %d, got %d", CallError, decoded.MessageTypeID)
	}
	if decoded.ErrorCode != "NotSupported" {
		t.Fatalf("expected error code NotSupported, got %s", decoded.ErrorCode)
	}
	if decoded.ErrorDescription != "Action not supported" {
		t.Fatalf("expected error description 'Action not supported', got %s", decoded.ErrorDescription)
	}
}

func TestFrameString(t *testing.T) {
	f := &Frame{MessageTypeID: Call, UniqueID: "1", Action: "Heartbeat"}
	if s := f.String(); s != "CALL[1] Heartbeat" {
		t.Fatalf("unexpected string: %s", s)
	}
	f2 := &Frame{MessageTypeID: CallResult, UniqueID: "2"}
	if s := f2.String(); s != "CALLRESULT[2]" {
		t.Fatalf("unexpected string: %s", s)
	}
	f3 := &Frame{MessageTypeID: CallError, UniqueID: "3", ErrorCode: "X"}
	if s := f3.String(); s != "CALLERROR[3] X" {
		t.Fatalf("unexpected string: %s", s)
	}
}

func TestUnmarshalInvalidFrame(t *testing.T) {
	_, err := Unmarshal([]byte(`[]`))
	if err == nil {
		t.Fatal("expected error for empty frame")
	}
	_, err = Unmarshal([]byte(`[1, "id"]`))
	if err == nil {
		t.Fatal("expected error for unknown message type")
	}
	_, err = Unmarshal([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}
