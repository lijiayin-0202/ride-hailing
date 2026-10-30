package biz

import (
	"context"
	"errors"
	"time"
)

const (
	// DispatchStatusCreated 表示派单尝试已创建，尚未实际向车辆下发。
	DispatchStatusCreated int16 = 0
	// Pending 仅表示车辆已收到指令且仍处于 30 秒确认窗口，不能再次分配同一订单。
	DispatchStatusPending int16 = 1
	// Accepted 是终态：订单后续应进入接驾流程，不再生成补派任务。
	DispatchStatusAccepted int16 = 2
	// Rejected 与 TimedOut 都是可补派终态，由原子事务创建下一次尝试任务。
	DispatchStatusRejected int16 = 3
	DispatchStatusTimedOut int16 = 4
	// 上限避免车辆全不可用时形成无限派单循环；与产品确认的“最多三辆候选车”保持一致。
	maxDispatchAttempts = 3
	// 截止时间由服务端统一计算，避免依赖车辆端不一致的本地时钟。
	confirmationTimeout = 30 * time.Second
)

var (
	ErrOrderNotSchedulable         = errors.New("订单当前不可调度")
	ErrNoEligibleVehicle           = errors.New("没有可调度车辆")
	ErrDispatchAttemptLimitReached = errors.New("派单尝试次数已达上限")
	ErrInvalidDispatchResolution   = errors.New("派单结果状态无效")
)

// DispatchRequest 是创建一次派单尝试所需的最小订单事实。
type DispatchRequest struct {
	OrderID          int64
	ServiceAreaID    int64
	DispatchStrategy string
	Schedulable      bool
	Cancelled        bool
}

// CandidateVehicle 是数据层按既定规则排序后提供的候选车辆。
// Score 的计算归数据层处理，本层只使用既有顺序。
type CandidateVehicle struct {
	ID                  int64
	Score               float64
	PickupDistanceKM    float64
	EstimatedArrivalMin int32
}

// GeoPoint 表示内部调度计算使用的一对经纬度。
type GeoPoint struct {
	Longitude float64
	Latitude  float64
}

// DispatchRecord 表示订单向一辆候选车发起的一次派单尝试。
type DispatchRecord struct {
	ID                        int64
	DispatchNo                string
	OrderID                   int64
	VehicleID                 int64
	AttemptNo                 int16
	DispatchStrategy          string
	CandidateScore            float64
	EstimatedPickupDistanceKM float64
	EstimatedArrivalMin       int32
	Status                    int16
	DispatchedAt              *time.Time
	RespondedAt               *time.Time
	FailureReason             string
}

// DispatchResult 返回新建记录及不持久化的确认截止时间。
type DispatchResult struct {
	Record               DispatchRecord
	ConfirmationDeadline time.Time
	RemainingAttempts    int
}

// DispatchLocation 是一条派单指令固定的上下车位置快照。
type DispatchLocation struct {
	StationID int64
	Longitude float64
	Latitude  float64
	Address   string
}

// VehicleDispatchInstruction 是模块 3 发往模块 4 的指令事实。
type VehicleDispatchInstruction struct {
	Dispatch             DispatchRecord
	Pickup               DispatchLocation
	Dropoff              DispatchLocation
	ConfirmationDeadline time.Time
}

// VehicleDispatchGateway 定义向模块 4 下发派单指令的出站端口。
type VehicleDispatchGateway interface {
	Send(context.Context, VehicleDispatchInstruction) (bool, error)
}

// DispatchRecordRepository 定义派单记录持久化端口。
type DispatchRecordRepository interface {
	CountAttempts(context.Context, int64) (int, error)
	Create(context.Context, DispatchRecord) error
}

// AttemptedVehicleRepository 读取订单已有派单尝试所使用的车辆。
type AttemptedVehicleRepository interface {
	ListAttemptedVehicleIDs(context.Context, int64) ([]int64, error)
}

// DispatchStateRepository 定义派单状态流转和超时扫描端口。
type DispatchStateRepository interface {
	MarkDispatched(context.Context, int64, time.Time) (DispatchRecord, error)
	Resolve(context.Context, DispatchResolution, time.Time) (DispatchRecord, error)
	ListPendingBefore(context.Context, time.Time) ([]DispatchRecord, error)
}

// AtomicDispatchResolutionRepository 能将终态、订单事件和补派任务放在同一事务内。
type AtomicDispatchResolutionRepository interface {
	ResolveAndQueue(context.Context, DispatchResolution, time.Time) (DispatchRecord, error)
}

// DispatchResolution 表示车辆确认、拒绝或系统超时事实。
type DispatchResolution struct {
	DispatchID           int64
	OrderID              int64
	VehicleID            int64
	Status               int16
	FailureReason        string
	RespondedAtUnixMilli int64
}

// CandidateVehicleRepository 定义候选车辆读取端口。
type CandidateVehicleRepository interface {
	ListEligible(context.Context, int64, []int64) ([]CandidateVehicle, error)
}

// DispatchNumberGenerator 生成业务派单编号。
type DispatchNumberGenerator interface {
	New() string
}

// DispatchIDGenerator 生成与数据库 BIGINT 主键兼容的应用侧全局 ID。
type DispatchIDGenerator interface {
	New() int64
}

// Clock 使确认截止时间可测试。
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

// NewSystemClock 提供生产环境使用的当前时间。
func NewSystemClock() Clock {
	return systemClock{}
}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

// DispatchUsecase 编排单次派单尝试，不负责真实车辆指令或订单状态回写。
type DispatchUsecase struct {
	records    DispatchRecordRepository
	candidates CandidateVehicleRepository
	numbers    DispatchNumberGenerator
	ids        DispatchIDGenerator
	clock      Clock
}

// DispatchStateUsecase 编排派单状态流转，不执行跨模块副作用。
type DispatchStateUsecase struct {
	repository DispatchStateRepository
	clock      Clock
}

// NewDispatchStateUsecase 创建派单状态用例。
func NewDispatchStateUsecase(repository DispatchStateRepository, clock Clock) *DispatchStateUsecase {
	return &DispatchStateUsecase{repository: repository, clock: clock}
}

// NewDispatchUsecase 创建派单领域用例。
func NewDispatchUsecase(
	records DispatchRecordRepository,
	candidates CandidateVehicleRepository,
	numbers DispatchNumberGenerator,
	ids DispatchIDGenerator,
	clock Clock,
) *DispatchUsecase {
	return &DispatchUsecase{
		records:    records,
		candidates: candidates,
		numbers:    numbers,
		ids:        ids,
		clock:      clock,
	}
}

// CreateAttempt 创建一条待下发的派单尝试记录。
func (usecase *DispatchUsecase) CreateAttempt(ctx context.Context, request DispatchRequest) (DispatchResult, error) {
	if request.OrderID <= 0 || request.ServiceAreaID <= 0 || !request.Schedulable || request.Cancelled {
		return DispatchResult{}, ErrOrderNotSchedulable
	}

	// 先计算所有历史尝试，拒绝和超时也计入上限，防止同一订单反复扰动运力。
	attempts, err := usecase.records.CountAttempts(ctx, request.OrderID)
	if err != nil {
		return DispatchResult{}, err
	}
	if attempts >= maxDispatchAttempts {
		return DispatchResult{}, ErrDispatchAttemptLimitReached
	}

	// 补派必须排除历史候选车，才能满足“一次尝试对应一辆不同车辆”的规则。
	excludedVehicleIDs, err := usecase.attemptedVehicleIDs(ctx, request.OrderID)
	if err != nil {
		return DispatchResult{}, err
	}
	candidates, err := usecase.candidates.ListEligible(ctx, request.ServiceAreaID, excludedVehicleIDs)
	if err != nil {
		return DispatchResult{}, err
	}
	if len(candidates) == 0 {
		return DispatchResult{}, ErrNoEligibleVehicle
	}

	// 领域层只创建事实；真实车辆下发及订单状态同步交由 Outbox 在事务提交后异步执行。
	record := DispatchRecord{
		ID:               usecase.ids.New(),
		DispatchNo:       usecase.numbers.New(),
		OrderID:          request.OrderID,
		VehicleID:        candidates[0].ID,
		AttemptNo:        int16(attempts + 1),
		DispatchStrategy: request.DispatchStrategy,
		Status:           DispatchStatusCreated,
	}
	if err := usecase.records.Create(ctx, record); err != nil {
		return DispatchResult{}, err
	}

	return DispatchResult{
		Record:               record,
		ConfirmationDeadline: usecase.clock.Now().Add(confirmationTimeout),
		RemainingAttempts:    maxDispatchAttempts - attempts - 1,
	}, nil
}

// MarkDispatched 将已创建尝试置为等待车辆确认。
func (usecase *DispatchStateUsecase) MarkDispatched(ctx context.Context, dispatchID int64) (DispatchRecord, error) {
	return usecase.repository.MarkDispatched(ctx, dispatchID, usecase.clock.Now())
}

// Resolve 将待确认尝试置为确认、拒绝或超时终态。
func (usecase *DispatchStateUsecase) Resolve(ctx context.Context, resolution DispatchResolution) (DispatchRecord, error) {
	if resolution.DispatchID <= 0 || !isResolutionStatus(resolution.Status) {
		return DispatchRecord{}, ErrInvalidDispatchResolution
	}
	at := resolutionTime(resolution, usecase.clock.Now())
	// PostgreSQL 实现会把终态、订单事件和补派任务写入同一事务，内存实现则保留基础接口以便单测。
	if repository, found := usecase.repository.(AtomicDispatchResolutionRepository); found {
		return repository.ResolveAndQueue(ctx, resolution, at)
	}
	return usecase.repository.Resolve(ctx, resolution, at)
}

func resolutionTime(resolution DispatchResolution, fallback time.Time) time.Time {
	if resolution.RespondedAtUnixMilli <= 0 {
		return fallback
	}
	return time.UnixMilli(resolution.RespondedAtUnixMilli).UTC()
}

// ListTimeouts 返回确认窗口已超过 30 秒的待确认尝试。
func (usecase *DispatchStateUsecase) ListTimeouts(ctx context.Context) ([]DispatchRecord, error) {
	return usecase.repository.ListPendingBefore(ctx, usecase.clock.Now().Add(-confirmationTimeout))
}

// ExpireTimeouts 将确认窗口已到期的待确认派单转换为超时终态。
func (usecase *DispatchStateUsecase) ExpireTimeouts(ctx context.Context) (int, error) {
	records, err := usecase.ListTimeouts(ctx)
	if err != nil {
		return 0, err
	}
	for _, record := range records {
		// 统一走 Resolve，确保超时与车辆拒绝使用完全相同的原子补派逻辑。
		_, err := usecase.Resolve(ctx, DispatchResolution{DispatchID: record.ID, OrderID: record.OrderID, VehicleID: record.VehicleID, Status: DispatchStatusTimedOut, FailureReason: "confirmation timeout"})
		if err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func (usecase *DispatchUsecase) attemptedVehicleIDs(ctx context.Context, orderID int64) ([]int64, error) {
	repository, found := usecase.records.(AttemptedVehicleRepository)
	if !found {
		return nil, nil
	}
	return repository.ListAttemptedVehicleIDs(ctx, orderID)
}

func isResolutionStatus(status int16) bool {
	return status == DispatchStatusAccepted || status == DispatchStatusRejected || status == DispatchStatusTimedOut
}
