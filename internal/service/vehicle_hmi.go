package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// VehicleHMIService 负责协议参数转换，并调用车辆 HMI 业务用例。
type VehicleHMIService struct {
	v1.UnimplementedVehicleHMIServiceServer
	uc  *biz.VehicleHMIUsecase
	log *log.Helper
}

// NewVehicleHMIService 创建车辆 HMI 接口服务。go run ./cmd/ride-hailing -conf ./configs/config.local.yaml
func NewVehicleHMIService(uc *biz.VehicleHMIUsecase, logger log.Logger) *VehicleHMIService {
	return &VehicleHMIService{uc: uc, log: log.NewHelper(logger)}
}

// GetPendingCommands 返回车辆待确认或待执行的指令。
func (s *VehicleHMIService) GetPendingCommands(ctx context.Context, req *v1.GetPendingCommandsRequest) (*v1.GetPendingCommandsResponse, error) {
	commands, err := s.uc.PendingCommands(ctx, req.GetVehicleId(), req.GetCommandNo())
	if err != nil {
		return nil, s.toServiceError(ctx, "get_pending_commands", err)
	}
	return &v1.GetPendingCommandsResponse{Commands: commandsToPB(commands)}, nil
}

// AckCommand 处理车辆收到指令后的确认请求。
func (s *VehicleHMIService) AckCommand(ctx context.Context, req *v1.AckCommandRequest) (*v1.AckCommandResponse, error) {
	command, err := s.uc.AcknowledgeCommand(ctx, req.GetCommandNo())
	if err != nil {
		return nil, s.toServiceError(ctx, "ack_command", err)
	}
	return &v1.AckCommandResponse{Command: commandToPB(command)}, nil
}

// ReportCommandResult 处理车辆指令的最终成功或失败结果。
func (s *VehicleHMIService) ReportCommandResult(ctx context.Context, req *v1.ReportCommandResultRequest) (*v1.ReportCommandResultResponse, error) {
	command, err := s.uc.CompleteCommand(ctx, req.GetCommandNo(), req.GetSucceeded(), req.GetResultMessage())
	if err != nil {
		return nil, s.toServiceError(ctx, "report_command_result", err)
	}
	return &v1.ReportCommandResultResponse{Command: commandToPB(command)}, nil
}

// VerifyBoarding 记录手机号、NFC 或动态码的上车核验结果。
func (s *VehicleHMIService) VerifyBoarding(ctx context.Context, req *v1.VerifyBoardingRequest) (*v1.VerifyBoardingResponse, error) {
	item := &biz.BoardingVerification{OrderID: req.GetOrderId(), VerificationType: req.GetVerificationType(), CredentialHash: req.GetCredentialHash(), Succeeded: req.GetSucceeded(), FailureReason: req.GetFailureReason()}
	if err := s.uc.VerifyBoarding(ctx, item); err != nil {
		return nil, s.toServiceError(ctx, "verify_boarding", err)
	}
	result := int32(0)
	// 对外响应使用 1 表示核验成功，0 表示核验失败。
	if item.Succeeded {
		result = 1
	}
	return &v1.VerifyBoardingResponse{AttemptResult: result, FailureReason: item.FailureReason, VerifiedAt: timestamppb.New(item.VerifiedAt)}, nil
}

// ReportTelemetry 将协议层遥测字段转换为业务实体并保存。
func (s *VehicleHMIService) ReportTelemetry(ctx context.Context, req *v1.ReportTelemetryRequest) (*v1.ReportTelemetryResponse, error) {
	item := &biz.TelemetryReport{
		VehicleID: req.GetVehicleId(), RelatedOrderID: req.GetRelatedOrderId(),
		Longitude: req.GetLongitude(), Latitude: req.GetLatitude(), SpeedKMH: req.GetSpeedKmh(),
		OperationStatus: req.GetOperationStatus(), OnlineStatus: req.GetOnlineStatus(),
		BatteryPercent: req.GetBatteryPercent(), SoftwareVersion: req.GetSoftwareVersion(),
		TelemetryData: marshalStruct(req.GetTelemetryData()), HeadingDegree: req.GetHeadingDegree(),
		PositionAccuracyM: req.GetPositionAccuracyM(), RouteProgress: req.GetRouteProgressPercent(),
		RemainingDistanceKM: req.GetRemainingDistanceKm(), ETAAt: timestampToTime(req.GetEtaAt()),
		RemainingRangeKM: req.GetRemainingRangeKm(), DoorStatus: marshalStruct(req.GetDoorStatus()),
		SeatStatus: marshalStruct(req.GetSeatStatus()), CabinTemperature: req.GetCabinTemperature(),
		NetworkSignal: req.GetNetworkSignal(), WarningCodes: req.GetWarningCodes(),
	}
	if err := s.uc.ReportTelemetry(ctx, item); err != nil {
		return nil, s.toServiceError(ctx, "report_telemetry", err)
	}
	return &v1.ReportTelemetryResponse{ReportedAt: timestamppb.New(item.ReportedAt)}, nil
}

// ReportFault 接收车辆故障及故障快照上报。
func (s *VehicleHMIService) ReportFault(ctx context.Context, req *v1.ReportFaultRequest) (*v1.ReportFaultResponse, error) {
	item := &biz.FaultReport{
		FaultNo: req.GetFaultNo(), VehicleID: req.GetVehicleId(), RelatedOrderID: req.GetRelatedOrderId(),
		FaultCode: req.GetFaultCode(), FaultLevel: req.GetFaultLevel(),
		FaultDescription: req.GetFaultDescription(), FaultData: marshalStruct(req.GetFaultData()),
		Status: req.GetStatus(),
	}
	// 客户端传入发生时间时使用客户端时间，否则由业务层补为服务端时间。
	if req.GetOccurredAt() != nil {
		item.OccurredAt = req.GetOccurredAt().AsTime()
	}
	if err := s.uc.ReportFault(ctx, item); err != nil {
		return nil, s.toServiceError(ctx, "report_fault", err)
	}
	return &v1.ReportFaultResponse{ReportedAt: timestamppb.New(item.ReportedAt)}, nil
}

// GetHMIView 返回车内屏幕需要展示的订单聚合数据。
func (s *VehicleHMIService) GetHMIView(ctx context.Context, req *v1.GetHMIViewRequest) (*v1.GetHMIViewResponse, error) {
	view, err := s.uc.HMIView(ctx, req.GetOrderId())
	if err != nil {
		return nil, s.toServiceError(ctx, "get_hmi_view", err)
	}
	return &v1.GetHMIViewResponse{
		OrderId: view.OrderID, OrderStatus: view.OrderStatus, VehicleId: view.VehicleID,
		PickupSnapshot: unmarshalStruct(view.PickupSnapshot), DropoffSnapshot: unmarshalStruct(view.DropoffSnapshot),
		PendingCommands: commandsToPB(view.PendingCommands), EstimatedAmount: view.EstimatedAmount,
		PayableAmount: view.PayableAmount, SafetyIncidents: safetyIncidentsToPB(view.SafetyIncidents),
	}, nil
}

// RequestAssistance 接收车辆靠边停车或远程协助请求。
func (s *VehicleHMIService) RequestAssistance(ctx context.Context, req *v1.RequestAssistanceRequest) (*v1.RequestAssistanceResponse, error) {
	item := &biz.AssistanceRequest{OrderID: req.GetOrderId(), VehicleID: req.GetVehicleId(), AssistanceType: req.GetAssistanceType(), Reason: req.GetReason()}
	if err := s.uc.RequestAssistance(ctx, item); err != nil {
		return nil, s.toServiceError(ctx, "request_assistance", err)
	}
	return &v1.RequestAssistanceResponse{RequestedAt: timestamppb.New(item.RequestedAt)}, nil
}

// ReportMediaRecord 登记车辆视频、音频或系统日志的文件元数据。
func (s *VehicleHMIService) ReportMediaRecord(ctx context.Context, req *v1.ReportMediaRecordRequest) (*v1.ReportMediaRecordResponse, error) {
	item := &biz.MediaRecord{
		VehicleID: req.GetVehicleId(), RelatedOrderID: req.GetRelatedOrderId(), MediaType: req.GetMediaType(),
		StorageKey: req.GetStorageKey(), FileSizeBytes: req.GetFileSizeBytes(),
		EndedAt: timestampToTime(req.GetEndedAt()), RetentionUntil: timestampToTime(req.GetRetentionUntil()),
		AccessLevel: req.GetAccessLevel(),
	}
	// 客户端未提供媒体开始时间时，由业务层使用服务端接收时间补齐。
	if req.GetStartedAt() != nil {
		item.StartedAt = req.GetStartedAt().AsTime()
	}
	if err := s.uc.ReportMediaRecord(ctx, item); err != nil {
		return nil, s.toServiceError(ctx, "report_media_record", err)
	}
	return &v1.ReportMediaRecordResponse{RecordedAt: timestamppb.New(item.RecordedAt)}, nil
}

// toServiceError 统一记录错误并转换为 Kratos 标准响应。
func (s *VehicleHMIService) toServiceError(ctx context.Context, operation string, err error) error {
	businessErr := biz.ToBusinessError(err)
	s.log.WithContext(ctx).Errorw("message", "vehicle hmi rpc failed", "operation", operation, "error_reason", businessErr.Reason)
	return biz.ToTransportError(businessErr)
}

// commandsToPB 批量转换车辆指令为 protobuf 响应对象。
func commandsToPB(commands []*biz.VehicleCommand) []*v1.VehicleCommand {
	result := make([]*v1.VehicleCommand, 0, len(commands))
	for _, command := range commands {
		result = append(result, commandToPB(command))
	}
	return result
}

// commandToPB 转换单条车辆指令为 protobuf 响应对象。
func commandToPB(command *biz.VehicleCommand) *v1.VehicleCommand {
	if command == nil {
		return nil
	}
	result := &v1.VehicleCommand{CommandNo: command.CommandNo, VehicleId: command.VehicleID, RelatedOrderId: command.RelatedOrderID, CommandType: command.CommandType, CommandPayload: unmarshalStruct(command.CommandPayload), Priority: command.Priority, Status: int32(command.Status), IssuedAt: timestamppb.New(command.IssuedAt), ResultMessage: command.ResultMessage}
	if command.AcknowledgedAt != nil {
		result.AcknowledgedAt = timestamppb.New(*command.AcknowledgedAt)
	}
	if command.FinishedAt != nil {
		result.FinishedAt = timestamppb.New(*command.FinishedAt)
	}
	return result
}

// safetyIncidentsToPB 转换 HMI 安全事件列表。
func safetyIncidentsToPB(items []*biz.SafetyIncident) []*v1.HMISafetyIncident {
	result := make([]*v1.HMISafetyIncident, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, &v1.HMISafetyIncident{
			IncidentNo: item.IncidentNo, IncidentType: item.IncidentType, Severity: item.Severity,
			Description: item.Description, Status: item.Status, CreatedAt: timestamppb.New(item.CreatedAt),
		})
	}
	return result
}

// timestampToTime 将可选 protobuf 时间转换为 Go 时间指针。
func timestampToTime(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	result := value.AsTime()
	return &result
}

// marshalStruct 将动态 protobuf 结构转换为 JSON 数据。
func marshalStruct(value *structpb.Struct) []byte {
	if value == nil {
		return []byte(`{}`)
	}
	body, err := json.Marshal(value.AsMap())
	if err != nil {
		return []byte(`{}`)
	}
	return body
}

// unmarshalStruct 将 JSON 数据转换为动态 protobuf 结构。
func unmarshalStruct(data []byte) *structpb.Struct {
	var values map[string]any
	if len(data) == 0 || json.Unmarshal(data, &values) != nil {
		return &structpb.Struct{}
	}
	value, err := structpb.NewStruct(values)
	if err != nil {
		return &structpb.Struct{}
	}
	return value
}
