package ocpp16

import "encoding/json"

// BootNotificationReq BootNotification 请求
type BootNotificationReq struct {
	ChargePointModel        string `json:"chargePointModel"`
	ChargePointVendor       string `json:"chargePointVendor"`
	ChargePointSerialNumber string `json:"chargePointSerialNumber,omitempty"`
	FirmwareVersion         string `json:"firmwareVersion,omitempty"`
	MeterType               string `json:"meterType,omitempty"`
}

// BootNotificationConf BootNotification 响应
type BootNotificationConf struct {
	Status      string `json:"status"`
	CurrentTime string `json:"currentTime"`
	Interval    int    `json:"interval"`
}

// StatusNotificationReq StatusNotification 请求
type StatusNotificationReq struct {
	ConnectorID int    `json:"connectorId"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// StatusNotificationConf StatusNotification 响应
type StatusNotificationConf struct{}

// HeartbeatReq Heartbeat 请求
type HeartbeatReq struct{}

// HeartbeatConf Heartbeat 响应
type HeartbeatConf struct {
	CurrentTime string `json:"currentTime"`
}

// AuthorizeReq Authorize 请求
type AuthorizeReq struct {
	IDTag string `json:"idTag"`
}

// AuthorizeConf Authorize 响应
type AuthorizeConf struct {
	IDTagInfo IDTagInfo `json:"idTagInfo"`
}

// IDTagInfo 授权标签信息
type IDTagInfo struct {
	Status string `json:"status"`
}

// StartTransactionReq StartTransaction 请求
type StartTransactionReq struct {
	ConnectorID int    `json:"connectorId"`
	IDTag       string `json:"idTag"`
	MeterStart  int    `json:"meterStart"`
	Timestamp   string `json:"timestamp"`
}

// StartTransactionConf StartTransaction 响应
type StartTransactionConf struct {
	TransactionID int       `json:"transactionId"`
	IDTagInfo     IDTagInfo `json:"idTagInfo"`
}

// StopTransactionReq StopTransaction 请求
type StopTransactionReq struct {
	TransactionID int    `json:"transactionId"`
	IDTag         string `json:"idTag"`
	MeterStop     int    `json:"meterStop"`
	Timestamp     string `json:"timestamp"`
}

// StopTransactionConf StopTransaction 响应
type StopTransactionConf struct {
	IDTagInfo IDTagInfo `json:"idTagInfo,omitempty"`
}

// MeterValuesReq MeterValues 请求
type MeterValuesReq struct {
	ConnectorID   int          `json:"connectorId"`
	TransactionID int          `json:"transactionId,omitempty"`
	MeterValue    []MeterValue `json:"meterValue"`
}

// MeterValue 电表读数
type MeterValue struct {
	Timestamp    string         `json:"timestamp"`
	SampledValue []SampledValue `json:"sampledValue"`
}

// SampledValue 采样值
type SampledValue struct {
	Value     string `json:"value"`
	Context   string `json:"context,omitempty"`
	Format    string `json:"format,omitempty"`
	Measurand string `json:"measurand,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Location  string `json:"location,omitempty"`
	Unit      string `json:"unit,omitempty"`
}

// RemoteStartTransactionReq 远程启动请求
type RemoteStartTransactionReq struct {
	ConnectorID int    `json:"connectorId,omitempty"`
	IDTag       string `json:"idTag"`
}

// RemoteStartTransactionConf 远程启动响应
type RemoteStartTransactionConf struct {
	Status string `json:"status"`
}

// RemoteStopTransactionReq 远程停止请求
type RemoteStopTransactionReq struct {
	TransactionID int `json:"transactionId"`
}

// RemoteStopTransactionConf 远程停止响应
type RemoteStopTransactionConf struct {
	Status string `json:"status"`
}

// SetChargingProfileReq SetChargingProfile 请求
type SetChargingProfileReq struct {
	ConnectorID        int               `json:"connectorId"`
	CSChargingProfiles CSChargingProfile `json:"csChargingProfiles"`
}

// CSChargingProfile 中央系统充电 profile
type CSChargingProfile struct {
	ChargingProfileID      int              `json:"chargingProfileId"`
	StackLevel             int              `json:"stackLevel"`
	ChargingProfilePurpose string           `json:"chargingProfilePurpose"`
	ChargingProfileKind    string           `json:"chargingProfileKind"`
	ChargingSchedule       ChargingSchedule `json:"chargingSchedule"`
}

// ChargingSchedule 充电计划
type ChargingSchedule struct {
	Duration               int                      `json:"duration,omitempty"`
	StartSchedule          string                   `json:"startSchedule,omitempty"`
	ChargingRateUnit       string                   `json:"chargingRateUnit"`
	ChargingSchedulePeriod []ChargingSchedulePeriod `json:"chargingSchedulePeriod"`
	MinChargingRate        float64                  `json:"minChargingRate,omitempty"`
}

// ChargingSchedulePeriod 充电计划周期
type ChargingSchedulePeriod struct {
	StartPeriod  int     `json:"startPeriod"`
	Limit        float64 `json:"limit"`
	NumberPhases int     `json:"numberPhases,omitempty"`
}

// SetChargingProfileConf SetChargingProfile 响应
type SetChargingProfileConf struct {
	Status string `json:"status"`
}

// ClearChargingProfileReq ClearChargingProfile 请求
type ClearChargingProfileReq struct {
	ID                     int    `json:"id,omitempty"`
	ConnectorID            int    `json:"connectorId,omitempty"`
	ChargingProfilePurpose string `json:"chargingProfilePurpose,omitempty"`
	StackLevel             int    `json:"stackLevel,omitempty"`
}

// ClearChargingProfileConf ClearChargingProfile 响应
type ClearChargingProfileConf struct {
	Status string `json:"status"`
}

// ResetReq Reset 请求
type ResetReq struct {
	Type string `json:"type"`
}

// ResetConf Reset 响应
type ResetConf struct {
	Status string `json:"status"`
}

// ChangeAvailabilityReq ChangeAvailability 请求
type ChangeAvailabilityReq struct {
	ConnectorID int    `json:"connectorId"`
	Type        string `json:"type"`
}

// ChangeAvailabilityConf ChangeAvailability 响应
type ChangeAvailabilityConf struct {
	Status string `json:"status"`
}

// ParsePayload 解析 payload 到目标结构
func ParsePayload(raw json.RawMessage, target interface{}) error {
	return json.Unmarshal(raw, target)
}

// BuildPayload 构建 payload 字节
func BuildPayload(src interface{}) (json.RawMessage, error) {
	return json.Marshal(src)
}
