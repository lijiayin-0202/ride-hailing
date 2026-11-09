package biz

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrInvalidCommandTransition = errors.New("invalid vehicle command transition")
	ErrInvalidVehicleInput      = errors.New("invalid vehicle hmi input")
)

const vehicleHMITracerName = "github.com/lijayin-0202/radish-ride-service/internal/biz/vehicle_hmi"

// VehicleHMIUsecase 编排车辆指令、核验、遥测和故障等领域逻辑。
type VehicleHMIUsecase struct {
	repo VehicleHMIRepo
	log  *log.Helper
}

// NewVehicleHMIUsecase 创建车辆 HMI 业务用例。
func NewVehicleHMIUsecase(repo VehicleHMIRepo, logger log.Logger) *VehicleHMIUsecase {
	return &VehicleHMIUsecase{repo: repo, log: log.NewHelper(logger)}
}

// PendingCommands 获取车辆未完成的指令，并按优先级和签发时间排序。
func (uc *VehicleHMIUsecase) PendingCommands(ctx context.Context, vehicleID int64, commandNo string) (commands []*VehicleCommand, err error) {
	ctx, span := uc.startSpan(ctx, "pending_commands")
	defer uc.finishSpan(ctx, span, "pending_commands", &err)
	// 车辆 ID 必须为有效的正整数，避免查询无效车辆的指令。
	if vehicleID <= 0 {
		return nil, ErrInvalidVehicleInput
	}
	commands, err = uc.repo.ListPendingCommands(ctx, vehicleID, commandNo)
	if err != nil {
		return nil, err
	}
	sortCommands(commands)
	return commands, nil
}

// AcknowledgeCommand 将已发送指令推进到车辆已确认状态。
func (uc *VehicleHMIUsecase) AcknowledgeCommand(ctx context.Context, commandNo string) (command *VehicleCommand, err error) {
	ctx, span := uc.startSpan(ctx, "acknowledge_command")
	defer uc.finishSpan(ctx, span, "acknowledge_command", &err)
	command, _, err = uc.transitionCommand(ctx, commandNo, CommandStatusAcknowledged, "")
	return command, err
}

// CompleteCommand 记录指令成功或失败的最终执行结果。
func (uc *VehicleHMIUsecase) CompleteCommand(ctx context.Context, commandNo string, succeeded bool, result string) (command *VehicleCommand, err error) {
	ctx, span := uc.startSpan(ctx, "complete_command")
	defer uc.finishSpan(ctx, span, "complete_command", &err)
	target := CommandStatusFailed
	// 车辆明确上报执行成功时，才把目标状态设置为成功。
	if succeeded {
		target = CommandStatusSucceeded
	}
	command, _, err = uc.transitionCommand(ctx, commandNo, target, result)
	if err != nil {
		return nil, err
	}
	// 终态重复上报仍发布同一业务事件，确保首次 Kafka 失败后可由车辆重试补发。
	if err = uc.repo.Publish(ctx, "vehicle.command.result", command.CommandNo, marshalEventPayload(command)); err != nil {
		return nil, err
	}
	return command, nil
}

// transitionCommand 校验状态流转并通过条件更新保证重复请求幂等。
func (uc *VehicleHMIUsecase) transitionCommand(ctx context.Context, commandNo string, target CommandStatus, result string) (*VehicleCommand, bool, error) {
	// 指令编号不能为空，否则无法定位需要更新的车辆指令。
	if commandNo == "" {
		return nil, false, ErrInvalidVehicleInput
	}
	command, err := uc.repo.GetCommand(ctx, commandNo)
	if err != nil {
		return nil, false, err
	}
	// 重复上报同一终态时直接返回，保证车辆重试请求幂等。
	if isDuplicateTransition(command.Status, target) {
		return command, false, nil
	}
	// 只允许“已发送→已确认→成功/失败”的顺序流转。
	if !canTransition(command.Status, target) {
		return nil, false, ErrInvalidCommandTransition
	}
	command, err = uc.repo.UpdateCommandStatus(ctx, commandNo, command.Status, target, result, time.Now().UTC())
	if err == nil {
		return command, true, nil
	}
	// 条件更新失败可能是并发请求已经完成了相同流转，重新读取后按幂等成功处理。
	latest, latestErr := uc.repo.GetCommand(ctx, commandNo)
	if latestErr == nil && isDuplicateTransition(latest.Status, target) {
		return latest, false, nil
	}
	return nil, false, err
}

// VerifyBoarding 校验基础参数并保存上车核验记录。
func (uc *VehicleHMIUsecase) VerifyBoarding(ctx context.Context, item *BoardingVerificationAttempt) (err error) {
	ctx, span := uc.startSpan(ctx, "verify_boarding")
	defer uc.finishSpan(ctx, span, "verify_boarding", &err)
	// 上车动态码原始值应为 4 位；当前接口只接收脱敏后的凭证哈希，因此这里只校验核验方式和哈希非空。
	if item == nil || item.OrderID <= 0 || item.VerificationType < 1 || item.VerificationType > 3 || item.CredentialHash == "" {
		return ErrInvalidVehicleInput
	}
	item.VerifiedAt = time.Now().UTC()
	return uc.repo.CreateBoardingVerification(ctx, item)
}

// ReportTelemetry 校验车辆实时数据并持久化遥测快照。
func (uc *VehicleHMIUsecase) ReportTelemetry(ctx context.Context, item *TelemetryReport) (err error) {
	ctx, span := uc.startSpan(ctx, "report_telemetry")
	defer uc.finishSpan(ctx, span, "report_telemetry", &err)
	// 遥测值必须处于物理和枚举允许范围，防止异常数据污染车辆实时状态。
	if item == nil || item.VehicleID <= 0 || !validCoordinates(item.Longitude, item.Latitude) ||
		item.SpeedKMH < 0 || item.OperationStatus < 0 || item.OperationStatus > 5 ||
		item.OnlineStatus < 0 || item.OnlineStatus > 1 || item.BatteryPercent < 0 || item.BatteryPercent > 100 ||
		item.HeadingDegree < 0 || item.HeadingDegree >= 360 || item.PositionAccuracyM < 0 ||
		item.RouteProgress < 0 || item.RouteProgress > 100 || item.RemainingDistanceKM < 0 ||
		item.RemainingRangeKM < 0 || item.NetworkSignal < 0 || item.NetworkSignal > 100 {
		return ErrInvalidVehicleInput
	}
	item.ReportedAt = time.Now().UTC()
	if err = uc.repo.CreateTelemetry(ctx, item); err != nil {
		return err
	}
	return uc.repo.Publish(ctx, "vehicle.telemetry.reported", strconv.FormatInt(item.VehicleID, 10), marshalEventPayload(item))
}

// ReportFault 校验并保存车辆故障，同时发送故障事件。
func (uc *VehicleHMIUsecase) ReportFault(ctx context.Context, item *FaultReport) (err error) {
	ctx, span := uc.startSpan(ctx, "report_fault")
	defer uc.finishSpan(ctx, span, "report_fault", &err)
	// 故障编号、车辆、故障内容不能为空，等级和状态必须使用约定枚举范围。
	if item == nil || item.FaultNo == "" || item.VehicleID <= 0 || item.FaultCode == "" || item.FaultDescription == "" ||
		item.FaultLevel < 1 || item.FaultLevel > 4 || item.Status < 0 || item.Status > 3 {
		return ErrInvalidVehicleInput
	}
	// 车辆未提供故障发生时间时，以服务端接收时间补齐。
	if item.OccurredAt.IsZero() {
		item.OccurredAt = time.Now().UTC()
	}
	item.ReportedAt = time.Now().UTC()
	if err = uc.repo.CreateFault(ctx, item); err != nil {
		return err
	}
	return uc.repo.Publish(ctx, "vehicle.fault.reported", item.FaultNo, marshalEventPayload(item))
}

// HMIView 查询指定订单的车内 HMI 聚合展示数据。
func (uc *VehicleHMIUsecase) HMIView(ctx context.Context, orderID int64) (view *HMIView, err error) {
	ctx, span := uc.startSpan(ctx, "hmi_view")
	defer uc.finishSpan(ctx, span, "hmi_view", &err)
	// 订单 ID 必须有效，HMI 视图只能按具体订单聚合。
	if orderID <= 0 {
		return nil, ErrInvalidVehicleInput
	}
	view, err = uc.repo.GetHMIView(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if view.VehicleID <= 0 {
		return view, nil
	}
	// HMI 聚合查询只读取待发送和已发送指令，不改变指令下发状态。
	view.PendingCommands, err = uc.repo.ListActiveCommands(ctx, view.VehicleID, "")
	if err != nil {
		return nil, err
	}
	sortCommands(view.PendingCommands)
	return view, nil
}

// RequestAssistance 保存车辆协助请求并发送异步事件。
func (uc *VehicleHMIUsecase) RequestAssistance(ctx context.Context, item *AssistanceRequest) (err error) {
	ctx, span := uc.startSpan(ctx, "request_assistance")
	defer uc.finishSpan(ctx, span, "request_assistance", &err)
	// 仅接受靠边停车或远程协助两类请求，并要求说明原因。
	if item == nil || item.OrderID <= 0 || item.VehicleID <= 0 ||
		(item.AssistanceType != 4 && item.AssistanceType != 5) || item.Reason == "" {
		return ErrInvalidVehicleInput
	}
	item.RequestedAt = time.Now().UTC()
	if err = uc.repo.CreateAssistanceRequest(ctx, item); err != nil {
		return err
	}
	return uc.repo.Publish(ctx, "vehicle.assistance.requested", strconv.FormatInt(item.OrderID, 10), marshalEventPayload(item))
}

// ReportMediaRecord 校验并登记媒体文件的存储元数据。
func (uc *VehicleHMIUsecase) ReportMediaRecord(ctx context.Context, item *MediaRecord) (err error) {
	ctx, span := uc.startSpan(ctx, "report_media_record")
	defer uc.finishSpan(ctx, span, "report_media_record", &err)
	// 媒体类型、访问级别和存储地址必须合法，文件大小不能为负数。
	if item == nil || item.VehicleID <= 0 || item.MediaType < 1 || item.MediaType > 3 || item.StorageKey == "" ||
		item.FileSizeBytes < 0 || item.AccessLevel < 1 || item.AccessLevel > 3 {
		return ErrInvalidVehicleInput
	}
	if item.StartedAt.IsZero() {
		item.StartedAt = time.Now().UTC()
	}
	// 媒体结束时间不能早于开始时间。
	if item.EndedAt != nil && item.EndedAt.Before(item.StartedAt) {
		return ErrInvalidVehicleInput
	}
	// 数据保留截止时间不能早于媒体开始时间。
	if item.RetentionUntil != nil && item.RetentionUntil.Before(item.StartedAt) {
		return ErrInvalidVehicleInput
	}
	item.RecordedAt = time.Now().UTC()
	return uc.repo.CreateMediaRecord(ctx, item)
}

// startSpan 为车辆 HMI 操作创建链路追踪节点。
func (uc *VehicleHMIUsecase) startSpan(ctx context.Context, operation string) (context.Context, trace.Span) {
	return otel.Tracer(vehicleHMITracerName).Start(ctx, "vehicle_hmi."+operation)
}

// finishSpan 根据执行结果结束追踪节点并记录结构化日志。
func (uc *VehicleHMIUsecase) finishSpan(ctx context.Context, span trace.Span, operation string, operationErr *error) {
	if *operationErr != nil {
		businessErr := ToBusinessError(*operationErr)
		span.RecordError(*operationErr)
		span.SetStatus(codes.Error, businessErr.Reason)
		uc.log.WithContext(ctx).Errorw("message", "vehicle hmi operation failed", "operation", operation, "error_reason", businessErr.Reason)
	} else {
		uc.log.WithContext(ctx).Infow("message", "vehicle hmi operation completed", "operation", operation)
	}
	span.End()
}

// canTransition 判断车辆指令是否允许从当前状态进入目标状态。
func canTransition(from, to CommandStatus) bool {
	return (from == CommandStatusSent && to == CommandStatusAcknowledged) ||
		(from == CommandStatusAcknowledged && (to == CommandStatusSucceeded || to == CommandStatusFailed))
}

// isDuplicateTransition 判断请求是否为可安全忽略的重复状态上报。
func isDuplicateTransition(from, to CommandStatus) bool {
	return from == to && (to == CommandStatusAcknowledged || to == CommandStatusSucceeded || to == CommandStatusFailed)
}

// sortCommands 按优先级和签发时间稳定排序车辆指令。
func sortCommands(commands []*VehicleCommand) {
	sort.SliceStable(commands, func(i, j int) bool {
		if commands[i].Priority == commands[j].Priority {
			return commands[i].IssuedAt.Before(commands[j].IssuedAt)
		}
		return commands[i].Priority < commands[j].Priority
	})
}

// validCoordinates 校验经纬度是否处于合法范围。
func validCoordinates(longitude, latitude float64) bool {
	return longitude >= -180 && longitude <= 180 && latitude >= -90 && latitude <= 90
}

// marshalEventPayload 将业务对象转换为 Kafka 事件载荷。
func marshalEventPayload(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}
