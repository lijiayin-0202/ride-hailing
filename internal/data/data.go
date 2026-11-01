package data

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	dataKafka "github.com/lijayin-0202/radish-ride-service/internal/data/kafka"
)

var ProviderSet = wire.NewSet(
	NewData,
	NewVehicleHMIRepo,
	wire.Bind(new(biz.VehicleHMIRepo), new(*VehicleHMIRepo)),
)

var (
	ErrDatabaseUnavailable = errors.New("postgresql database is unavailable")
	ErrKafkaUnavailable    = errors.New("kafka producer is unavailable")
)

// Data 集中管理 PostgreSQL、Kafka 和日志等数据层资源。
type Data struct {
	db     *pgxpool.Pool
	writer *dataKafka.Writer
	log    *log.Helper
}

// NewData 根据配置初始化数据层连接，并返回统一清理函数。
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	data := &Data{log: log.NewHelper(logger)}
	if c.GetDatabase().GetDriver() == "postgres" || c.GetDatabase().GetDriver() == "postgresql" {
		pool, err := pgxpool.New(context.TODO(), c.GetDatabase().GetSource())
		if err != nil {
			return nil, nil, err
		}
		data.db = pool
	}
	if brokers := c.GetKafka().GetBrokers(); len(brokers) > 0 {
		data.writer = dataKafka.NewWriter(brokers)
	}
	cleanup := func() {
		if data.writer != nil {
			if err := data.writer.Close(); err != nil {
				data.log.Errorw("message", "close kafka writer failed", "error", err)
			}
		}
		if data.db != nil {
			data.db.Close()
		}
		data.log.Info("closing the data resources")
	}
	return data, cleanup, nil
}

// VehicleHMIRepo 实现车辆 HMI 业务所需的 PostgreSQL 和 Kafka 操作。
type VehicleHMIRepo struct {
	data *Data
}

// NewVehicleHMIRepo 创建车辆 HMI 数据仓库。
func NewVehicleHMIRepo(data *Data) *VehicleHMIRepo {
	return &VehicleHMIRepo{data: data}
}

// pool 获取数据库连接池，未配置 PostgreSQL 时返回明确错误。
func (r *VehicleHMIRepo) pool() (*pgxpool.Pool, error) {
	if r.data.db == nil {
		return nil, ErrDatabaseUnavailable
	}
	return r.data.db, nil
}

// CreateBoardingVerification 在事务中保存核验记录并推进成功订单状态。
func (r *VehicleHMIRepo) CreateBoardingVerification(ctx context.Context, item *biz.BoardingVerification) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := 0
	// 核验成功记录为 1，失败记录为 0。
	if item.Succeeded {
		result = 1
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.boarding_verifications (order_id, verification_type, credential_hash, attempt_result, failure_reason, verified_at) VALUES ($1, $2, $3, $4, $5, $6)`, item.OrderID, item.VerificationType, item.CredentialHash, result, item.FailureReason, item.VerifiedAt); err != nil {
		return err
	}
	// 只有核验成功才把订单从待上车状态推进到行程中。
	if item.Succeeded {
		command, updateErr := tx.Exec(ctx, `UPDATE robotaxi.trip_orders SET status = 4, boarded_at = $2, updated_at = $2 WHERE id = $1 AND status = 3`, item.OrderID, item.VerifiedAt)
		if updateErr != nil {
			return updateErr
		}
		// 未更新到唯一订单说明订单状态已变化或订单不存在，禁止继续写状态日志。
		if command.RowsAffected() != 1 {
			return biz.ErrInvalidCommandTransition
		}
		if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.trip_status_logs (order_id, previous_status, current_status, change_source, created_at) VALUES ($1, 3, 4, 2, $2)`, item.OrderID, item.VerifiedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CreateTelemetry 在同一事务内更新车辆快照并写入位置、状态和遥测日志。
func (r *VehicleHMIRepo) CreateTelemetry(ctx context.Context, item *biz.TelemetryReport) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previousStatus int32
	if err = tx.QueryRow(ctx, `SELECT operation_status FROM robotaxi.vehicles WHERE id = $1 FOR UPDATE`, item.VehicleID).Scan(&previousStatus); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE robotaxi.vehicles SET operation_status = $2, online_status = $3, battery_percent = $4, current_longitude = $5, current_latitude = $6, software_version = $7, last_reported_at = $8, updated_at = $8 WHERE id = $1`, item.VehicleID, item.OperationStatus, item.OnlineStatus, item.BatteryPercent, item.Longitude, item.Latitude, item.SoftwareVersion, item.ReportedAt); err != nil {
		return err
	}
	// 车辆运行状态发生变化时才追加状态变更日志。
	if previousStatus != item.OperationStatus {
		if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.vehicle_status_logs (vehicle_id, previous_status, current_status, related_order_id, change_source, changed_at) VALUES ($1, $2, $3, NULLIF($4, 0), 1, $5)`, item.VehicleID, previousStatus, item.OperationStatus, item.RelatedOrderID, item.ReportedAt); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.vehicle_location_logs (vehicle_id, order_id, longitude, latitude, heading_degree, speed_kmh, position_accuracy_m, reported_at) VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8)`, item.VehicleID, item.RelatedOrderID, item.Longitude, item.Latitude, item.HeadingDegree, item.SpeedKMH, item.PositionAccuracyM, item.ReportedAt); err != nil {
		return err
	}
	// 遥测关联有效订单时，同时写入行程轨迹日志。
	if item.RelatedOrderID > 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.trip_location_logs (order_id, vehicle_id, longitude, latitude, speed_kmh, route_progress_percent, remaining_distance_km, eta_at, reported_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, item.RelatedOrderID, item.VehicleID, item.Longitude, item.Latitude, item.SpeedKMH, item.RouteProgress, item.RemainingDistanceKM, item.ETAAt, item.ReportedAt); err != nil {
			return err
		}
	}
	warningCodes, err := json.Marshal(item.WarningCodes)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.vehicle_telemetry_logs (vehicle_id, order_id, battery_percent, remaining_range_km, door_status, seat_status, cabin_temperature, network_signal, warning_codes, reported_at) VALUES ($1, NULLIF($2, 0), $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9::jsonb, $10)`, item.VehicleID, item.RelatedOrderID, item.BatteryPercent, item.RemainingRangeKM, string(defaultJSON(item.DoorStatus)), string(defaultJSON(item.SeatStatus)), item.CabinTemperature, item.NetworkSignal, string(warningCodes), item.ReportedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateFault 保存车辆故障等级、描述和现场快照。
func (r *VehicleHMIRepo) CreateFault(ctx context.Context, item *biz.FaultReport) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.vehicle_faults (fault_no, vehicle_id, related_order_id, fault_code, fault_level, fault_description, fault_data, status, occurred_at, created_at, updated_at) VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7::jsonb, $8, $9, $10, $10)`, item.FaultNo, item.VehicleID, item.RelatedOrderID, item.FaultCode, item.FaultLevel, item.FaultDescription, string(defaultJSON(item.FaultData)), item.Status, item.OccurredAt, item.ReportedAt)
	return err
}

// GetHMIView 聚合订单、待执行指令和未关闭安全事件。
func (r *VehicleHMIRepo) GetHMIView(ctx context.Context, orderID int64) (*biz.HMIView, error) {
	pool, err := r.pool()
	if err != nil {
		return nil, err
	}
	view := &biz.HMIView{OrderID: orderID}
	err = pool.QueryRow(ctx, `SELECT status, COALESCE(vehicle_id, 0), pickup_snapshot::text, dropoff_snapshot::text, estimated_amount, payable_amount FROM robotaxi.trip_orders WHERE id = $1`, orderID).Scan(&view.OrderStatus, &view.VehicleID, &view.PickupSnapshot, &view.DropoffSnapshot, &view.EstimatedAmount, &view.PayableAmount)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT incident_no, incident_type, severity, description, status, created_at FROM robotaxi.safety_incidents WHERE order_id = $1 AND status IN (0, 1) ORDER BY severity DESC, created_at ASC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		incident := &biz.SafetyIncident{}
		if err = rows.Scan(&incident.IncidentNo, &incident.IncidentType, &incident.Severity, &incident.Description, &incident.Status, &incident.CreatedAt); err != nil {
			return nil, err
		}
		view.SafetyIncidents = append(view.SafetyIncidents, incident)
	}
	return view, rows.Err()
}

// CreateAssistanceRequest 将车辆协助请求记录为待处理安全事件。
func (r *VehicleHMIRepo) CreateAssistanceRequest(ctx context.Context, item *biz.AssistanceRequest) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	severity := 2
	// 靠边停车的安全影响更高，使用更高事件等级。
	if item.AssistanceType == 4 {
		severity = 3
	}
	incidentNo := "VHMI-" + uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.safety_incidents (incident_no, order_id, vehicle_id, incident_type, severity, description, snapshot_data, status, created_at, updated_at) VALUES ($1, $2, $3, 5, $4, $5, '{}'::jsonb, 0, $6, $6)`, incidentNo, item.OrderID, item.VehicleID, severity, item.Reason, item.RequestedAt)
	return err
}

// CreateMediaRecord 保存媒体文件的存储位置、时间范围和访问级别。
func (r *VehicleHMIRepo) CreateMediaRecord(ctx context.Context, item *biz.MediaRecord) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.vehicle_media_records (vehicle_id, related_order_id, media_type, storage_key, file_size_bytes, started_at, ended_at, retention_until, access_level, created_at) VALUES ($1, NULLIF($2, 0), $3, $4, NULLIF($5, 0), $6, $7, $8, $9, $10)`, item.VehicleID, item.RelatedOrderID, item.MediaType, item.StorageKey, item.FileSizeBytes, item.StartedAt, item.EndedAt, item.RetentionUntil, item.AccessLevel, item.RecordedAt)
	return err
}

// Publish 通过统一 Kafka 生产者发送车辆业务事件。
func (r *VehicleHMIRepo) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if r.data.writer == nil {
		return ErrKafkaUnavailable
	}
	if err := r.data.writer.Publish(ctx, topic, key, payload); err != nil {
		r.data.log.WithContext(ctx).Errorw(
			"message", "publish vehicle event failed",
			"topic", topic,
			"key", key,
			"error", err,
		)
		return err
	}
	return nil
}
