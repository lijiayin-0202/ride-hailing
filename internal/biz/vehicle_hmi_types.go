package biz

import (
	"context"
	"time"
)

// CommandStatus 表示车辆指令从待发送到执行结束的状态。
type CommandStatus int32

const (
	CommandStatusPending CommandStatus = iota
	CommandStatusSent
	CommandStatusAcknowledged
	CommandStatusSucceeded
	CommandStatusFailed
)

// VehicleCommand 是业务层使用的车辆指令实体。
type VehicleCommand struct {
	CommandNo      string
	VehicleID      int64
	RelatedOrderID int64
	CommandType    int32
	CommandPayload []byte
	Priority       int32
	Status         CommandStatus
	IssuedAt       time.Time
	AcknowledgedAt *time.Time
	FinishedAt     *time.Time
	ResultMessage  string
}

// BoardingVerificationAttempt 记录一次乘客上车核验尝试。
type BoardingVerificationAttempt struct {
	OrderID          int64
	VerificationType int32
	CredentialHash   string
	Succeeded        bool
	FailureReason    string
	VerifiedAt       time.Time
}

// TelemetryReport 汇总车辆位置、运行状态和实时遥测信息。
type TelemetryReport struct {
	VehicleID           int64
	RelatedOrderID      int64
	Longitude           float64
	Latitude            float64
	SpeedKMH            float64
	OperationStatus     int32
	OnlineStatus        int32
	BatteryPercent      float64
	SoftwareVersion     string
	TelemetryData       []byte
	HeadingDegree       float64
	PositionAccuracyM   float64
	RouteProgress       float64
	RemainingDistanceKM float64
	ETAAt               *time.Time
	RemainingRangeKM    float64
	DoorStatus          []byte
	SeatStatus          []byte
	CabinTemperature    float64
	NetworkSignal       int32
	WarningCodes        []string
	ReportedAt          time.Time
}

// FaultReport 表示车辆上报的一次故障及现场快照。
type FaultReport struct {
	FaultNo          string
	VehicleID        int64
	RelatedOrderID   int64
	FaultCode        string
	FaultLevel       int32
	FaultDescription string
	FaultData        []byte
	Status           int32
	OccurredAt       time.Time
	ReportedAt       time.Time
}

// AssistanceRequest 表示车辆发起的靠边停车或远程协助请求。
type AssistanceRequest struct {
	OrderID        int64
	VehicleID      int64
	AssistanceType int32
	Reason         string
	RequestedAt    time.Time
}

// MediaRecord 保存车辆媒体文件的存储元数据，不保存文件内容。
type MediaRecord struct {
	VehicleID      int64
	RelatedOrderID int64
	MediaType      int32
	StorageKey     string
	FileSizeBytes  int64
	StartedAt      time.Time
	EndedAt        *time.Time
	RetentionUntil *time.Time
	AccessLevel    int32
	RecordedAt     time.Time
}

// SafetyIncident 表示 HMI 需要展示的安全事件摘要。
type SafetyIncident struct {
	IncidentNo   string
	IncidentType int32
	Severity     int32
	Description  string
	Status       int32
	CreatedAt    time.Time
}

// HMIView 是订单、车辆、费用、指令和安全信息的聚合结果。
type HMIView struct {
	OrderID         int64
	OrderStatus     int32
	VehicleID       int64
	PickupSnapshot  []byte
	DropoffSnapshot []byte
	PendingCommands []*VehicleCommand
	EstimatedAmount float64
	PayableAmount   float64
	SafetyIncidents []*SafetyIncident
}

// VehicleHMIRepo 定义车辆 HMI 业务所需的数据访问能力。
type VehicleHMIRepo interface {
	ListPendingCommands(context.Context, int64, string) ([]*VehicleCommand, error)
	ListActiveCommands(context.Context, int64, string) ([]*VehicleCommand, error)
	GetCommand(context.Context, string) (*VehicleCommand, error)
	UpdateCommandStatus(context.Context, string, CommandStatus, CommandStatus, string, time.Time) (*VehicleCommand, error)
	CreateBoardingVerification(context.Context, *BoardingVerificationAttempt) error
	CreateTelemetry(context.Context, *TelemetryReport) error
	CreateFault(context.Context, *FaultReport) error
	GetHMIView(context.Context, int64) (*HMIView, error)
	CreateAssistanceRequest(context.Context, *AssistanceRequest) error
	CreateMediaRecord(context.Context, *MediaRecord) error
	Publish(context.Context, string, string, []byte) error
}
