package data

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// VehicleHMIRepo 使用现有 PostgreSQL 和 Kafka 资源实现车辆端数据访问。
type VehicleHMIRepo struct {
	data *Data
}

func NewVehicleHMIRepo(data *Data) *VehicleHMIRepo {
	return &VehicleHMIRepo{data: data}
}

func (r *VehicleHMIRepo) pool() (*pgxpool.Pool, error) {
	if r.data.db == nil {
		return nil, ErrDatabaseUnavailable
	}
	return r.data.db, nil
}

func (r *VehicleHMIRepo) CreateBoardingVerification(ctx context.Context, item *biz.BoardingVerificationAttempt) error {
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
	if item.Succeeded {
		result = 1
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.boarding_verifications (order_id, verification_type, credential_hash, attempt_result, failure_reason, verified_at) VALUES ($1, $2, $3, $4, $5, $6)`, item.OrderID, item.VerificationType, item.CredentialHash, result, item.FailureReason, item.VerifiedAt); err != nil {
		return err
	}
	if !item.Succeeded {
		return tx.Commit(ctx)
	}
	command, err := tx.Exec(ctx, `UPDATE robotaxi.trip_orders SET status = 4, boarded_at = $2, updated_at = $2 WHERE id = $1 AND status = 3`, item.OrderID, item.VerifiedAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return biz.ErrInvalidCommandTransition
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.trip_status_logs (order_id, previous_status, current_status, change_source, created_at) VALUES ($1, 3, 4, 2, $2)`, item.OrderID, item.VerifiedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

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
	if previousStatus != item.OperationStatus {
		if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.vehicle_status_logs (vehicle_id, previous_status, current_status, related_order_id, change_source, changed_at) VALUES ($1, $2, $3, NULLIF($4, 0), 1, $5)`, item.VehicleID, previousStatus, item.OperationStatus, item.RelatedOrderID, item.ReportedAt); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO robotaxi.vehicle_location_logs (vehicle_id, order_id, longitude, latitude, heading_degree, speed_kmh, position_accuracy_m, reported_at) VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8)`, item.VehicleID, item.RelatedOrderID, item.Longitude, item.Latitude, item.HeadingDegree, item.SpeedKMH, item.PositionAccuracyM, item.ReportedAt); err != nil {
		return err
	}
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

func (r *VehicleHMIRepo) CreateFault(ctx context.Context, item *biz.FaultReport) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.vehicle_faults (fault_no, vehicle_id, related_order_id, fault_code, fault_level, fault_description, fault_data, status, occurred_at, created_at, updated_at) VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7::jsonb, $8, $9, $10, $10)`, item.FaultNo, item.VehicleID, item.RelatedOrderID, item.FaultCode, item.FaultLevel, item.FaultDescription, string(defaultJSON(item.FaultData)), item.Status, item.OccurredAt, item.ReportedAt)
	return err
}

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

func (r *VehicleHMIRepo) CreateAssistanceRequest(ctx context.Context, item *biz.AssistanceRequest) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	severity := 2
	if item.AssistanceType == 4 {
		severity = 3
	}
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.safety_incidents (incident_no, order_id, vehicle_id, incident_type, severity, description, snapshot_data, status, created_at, updated_at) VALUES ($1, $2, $3, 5, $4, $5, '{}'::jsonb, 0, $6, $6)`, "VHMI-"+uuid.NewString(), item.OrderID, item.VehicleID, severity, item.Reason, item.RequestedAt)
	return err
}

func (r *VehicleHMIRepo) CreateMediaRecord(ctx context.Context, item *biz.MediaRecord) error {
	pool, err := r.pool()
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO robotaxi.vehicle_media_records (vehicle_id, related_order_id, media_type, storage_key, file_size_bytes, started_at, ended_at, retention_until, access_level, created_at) VALUES ($1, NULLIF($2, 0), $3, $4, NULLIF($5, 0), $6, $7, $8, $9, $10)`, item.VehicleID, item.RelatedOrderID, item.MediaType, item.StorageKey, item.FileSizeBytes, item.StartedAt, item.EndedAt, item.RetentionUntil, item.AccessLevel, item.RecordedAt)
	return err
}

func (r *VehicleHMIRepo) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if r.data.vehicleHMIWriter == nil {
		return ErrKafkaUnavailable
	}
	if err := r.data.vehicleHMIWriter.Publish(ctx, topic, key, payload); err != nil {
		r.data.log.WithContext(ctx).Errorw("message", "publish vehicle event failed", "topic", topic, "key", key, "error", err)
		return err
	}
	return nil
}

var _ biz.VehicleHMIRepo = (*VehicleHMIRepo)(nil)
