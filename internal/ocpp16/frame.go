package ocpp16

import (
	"encoding/json"
	"fmt"
)

// MessageType 常量
const (
	Call        = 2
	CallResult  = 3
	CallError   = 4
)

// Frame 是 OCPP 1.6 JSON 帧的通用表示
type Frame struct {
	MessageTypeID int
	UniqueID        string
	Action          string
	Payload         json.RawMessage
	ErrorCode       string
	ErrorDescription string
	ErrorDetails    map[string]interface{}
}

// Marshal 将 Frame 编码为 OCPP JSON 帧
func (f *Frame) Marshal() ([]byte, error) {
	switch f.MessageTypeID {
	case Call:
		return json.Marshal([]interface{}{f.MessageTypeID, f.UniqueID, f.Action, json.RawMessage(f.Payload)})
	case CallResult:
		return json.Marshal([]interface{}{f.MessageTypeID, f.UniqueID, json.RawMessage(f.Payload)})
	case CallError:
		return json.Marshal([]interface{}{f.MessageTypeID, f.UniqueID, f.ErrorCode, f.ErrorDescription, f.ErrorDetails})
	default:
		return nil, fmt.Errorf("unknown message type %d", f.MessageTypeID)
	}
}

// Unmarshal 解码 OCPP JSON 帧
func Unmarshal(data []byte) (*Frame, error) {
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal raw: %w", err)
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("frame too short: %d elements", len(raw))
	}
	msgTypeID, ok := raw[0].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid message type id: %T", raw[0])
	}
	uniqueID, ok := raw[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid unique id: %T", raw[1])
	}
	f := &Frame{
		MessageTypeID: int(msgTypeID),
		UniqueID:      uniqueID,
	}
	switch int(msgTypeID) {
	case Call:
		if len(raw) < 4 {
			return nil, fmt.Errorf("call frame too short: %d elements", len(raw))
		}
		action, ok := raw[2].(string)
		if !ok {
			return nil, fmt.Errorf("invalid action: %T", raw[2])
		}
		f.Action = action
		payload, _ := json.Marshal(raw[3])
		f.Payload = payload
	case CallResult:
		if len(raw) >= 3 {
			payload, _ := json.Marshal(raw[2])
			f.Payload = payload
		}
	case CallError:
		if len(raw) < 5 {
			return nil, fmt.Errorf("callerror frame too short: %d elements", len(raw))
		}
		code, _ := raw[2].(string)
		desc, _ := raw[3].(string)
		f.ErrorCode = code
		f.ErrorDescription = desc
		if details, ok := raw[4].(map[string]interface{}); ok {
			f.ErrorDetails = details
		}
	default:
		return nil, fmt.Errorf("unknown message type %d", int(msgTypeID))
	}
	return f, nil
}

// String 返回帧的简要描述
func (f *Frame) String() string {
	switch f.MessageTypeID {
	case Call:
		return fmt.Sprintf("CALL[%s] %s", f.UniqueID, f.Action)
	case CallResult:
		return fmt.Sprintf("CALLRESULT[%s]", f.UniqueID)
	case CallError:
		return fmt.Sprintf("CALLERROR[%s] %s", f.UniqueID, f.ErrorCode)
	default:
		return fmt.Sprintf("UNKNOWN[%s] type=%d", f.UniqueID, f.MessageTypeID)
	}
}

// GenerateID 生成一个消息唯一 ID
func GenerateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, nowFunc())
}

var nowFunc = func() int64 {
	return 0
}
