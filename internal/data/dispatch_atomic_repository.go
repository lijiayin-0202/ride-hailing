package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// CreatePending 在单一事务中创建派单、模块 4 指令和模块 2 待确认事件。
func (repository *DispatchRepository) CreatePending(ctx context.Context, input biz.PendingDispatchAttempt) (biz.AssignDispatchResult, error) {
	if repository.data.dispatchIDGenerator == nil || input.AverageSpeedKMH <= 0 {
		return biz.AssignDispatchResult{}, fmt.Errorf("invalid pending dispatch input")
	}
	tx, err := repository.data.pool.Begin(ctx)
	if err != nil {
		return biz.AssignDispatchResult{}, fmt.Errorf("begin pending dispatch transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // 成功提交后返回 pgx.ErrTxClosed。

	// 第一步写派单事实；后续两个 Outbox 任何一个失败都会由事务整体回滚。
	record, err := repository.insertPendingDispatch(ctx, tx, input)
	if err != nil {
		if errors.Is(err, ErrDispatchAttemptConflict) {
			// 并发请求由数据库唯一索引兜底，回滚后读取活跃记录实现幂等返回。
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return biz.AssignDispatchResult{}, fmt.Errorf("rollback pending dispatch conflict: %w", rollbackErr)
			}
			return repository.reusedActiveDispatch(ctx, input.Snapshot.OrderID)
		}
		return biz.AssignDispatchResult{}, err
	}
	// 第二、三步分别写模块 4 指令和模块 2 阶段事件，二者不可互相替代。
	if err := repository.insertPendingOutboxes(ctx, tx, record, input); err != nil {
		return biz.AssignDispatchResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return biz.AssignDispatchResult{}, fmt.Errorf("commit pending dispatch transaction: %w", err)
	}
	return biz.AssignDispatchResult{Record: record, ConfirmationDeadline: input.Deadline}, nil
}

// FindActive 返回同一订单尚未终态的唯一派单尝试。
func (repository *DispatchRepository) FindActive(ctx context.Context, orderID int64) (biz.DispatchRecord, bool, error) {
	row := repository.data.pool.QueryRow(ctx, `SELECT id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy,
		candidate_score, estimated_pickup_distance_km, estimated_arrival_min, status, failure_reason, dispatched_at, responded_at FROM public.dispatch_records
		WHERE order_id = $1 AND status IN ($2, $3) ORDER BY created_at DESC, id DESC LIMIT 1`,
		orderID, biz.DispatchStatusCreated, biz.DispatchStatusPending)
	record, err := scanActiveDispatchRecord(row)
	if errors.Is(err, ErrDispatchStateConflict) {
		return biz.DispatchRecord{}, false, nil
	}
	if err != nil {
		return biz.DispatchRecord{}, false, fmt.Errorf("find active dispatch: %w", err)
	}
	return record, true, nil
}

func scanActiveDispatchRecord(row pgx.Row) (biz.DispatchRecord, error) {
	var record biz.DispatchRecord
	var score, distance *float64
	var arrival *int32
	var failureReason *string
	err := row.Scan(&record.ID, &record.DispatchNo, &record.OrderID, &record.VehicleID, &record.AttemptNo,
		&record.DispatchStrategy, &score, &distance, &arrival, &record.Status, &failureReason, &record.DispatchedAt, &record.RespondedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return biz.DispatchRecord{}, ErrDispatchStateConflict
	}
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	// 旧数据可能没有度量值；非空值使用数据库固定精度后的持久化结果。
	if score != nil {
		record.CandidateScore = *score
	}
	if distance != nil {
		record.EstimatedPickupDistanceKM = *distance
	}
	if arrival != nil {
		record.EstimatedArrivalMin = *arrival
	}
	if failureReason != nil {
		record.FailureReason = *failureReason
	}
	return record, nil
}

func (repository *DispatchRepository) reusedActiveDispatch(ctx context.Context, orderID int64) (biz.AssignDispatchResult, error) {
	record, found, err := repository.FindActive(ctx, orderID)
	if err != nil || !found {
		return biz.AssignDispatchResult{}, fmt.Errorf("read active dispatch after conflict: %w", err)
	}
	deadline := time.Now().UTC().Add(30 * time.Second)
	if record.DispatchedAt != nil {
		deadline = record.DispatchedAt.Add(30 * time.Second)
	}
	return biz.AssignDispatchResult{Record: record, ConfirmationDeadline: deadline, ReusedActiveAttempt: true}, nil
}

func (repository *DispatchRepository) insertPendingDispatch(ctx context.Context, tx pgx.Tx, input biz.PendingDispatchAttempt) (biz.DispatchRecord, error) {
	attemptCount, err := countDispatchAttempts(ctx, tx, input.Snapshot.OrderID)
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	// 一条订单最多只允许三次不同车辆的派单尝试。
	if attemptCount >= 3 {
		return biz.DispatchRecord{}, biz.ErrDispatchAttemptLimitReached
	}
	candidate, err := selectNearestCandidate(ctx, tx, input.Snapshot, input.AverageSpeedKMH)
	if err != nil {
		return biz.DispatchRecord{}, err
	}
	recordID := repository.data.dispatchIDGenerator.New()
	record := biz.DispatchRecord{
		ID: recordID, DispatchNo: fmt.Sprintf("DSP-%d", recordID), OrderID: input.Snapshot.OrderID,
		VehicleID: candidate.ID, AttemptNo: int16(attemptCount + 1), DispatchStrategy: input.Strategy,
		CandidateScore: candidate.Score, EstimatedPickupDistanceKM: candidate.PickupDistanceKM, EstimatedArrivalMin: candidate.EstimatedArrivalMin,
		Status: biz.DispatchStatusPending, DispatchedAt: timePointer(input.Deadline.Add(-30 * time.Second)),
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.dispatch_records
		(id, dispatch_no, order_id, vehicle_id, attempt_no, dispatch_strategy, candidate_score,
		 estimated_pickup_distance_km, estimated_arrival_min, status, dispatched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		record.ID, record.DispatchNo, record.OrderID, record.VehicleID, record.AttemptNo,
		record.DispatchStrategy, candidate.Score, candidate.PickupDistanceKM,
		candidate.EstimatedArrivalMin, record.Status, record.DispatchedAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return biz.DispatchRecord{}, ErrDispatchAttemptConflict
		}
		return biz.DispatchRecord{}, fmt.Errorf("insert pending dispatch: %w", err)
	}
	return record, nil
}

func (repository *DispatchRepository) insertPendingOutboxes(ctx context.Context, tx pgx.Tx, record biz.DispatchRecord, input biz.PendingDispatchAttempt) error {
	instructionPayload, err := json.Marshal(biz.VehicleDispatchInstruction{
		Dispatch: record, Pickup: input.Snapshot.Pickup, Dropoff: input.Snapshot.Dropoff, ConfirmationDeadline: input.Deadline,
	})
	if err != nil {
		return fmt.Errorf("marshal vehicle dispatch instruction: %w", err)
	}
	event := biz.OrderDispatchEvent{DispatchNo: record.DispatchNo, OrderID: record.OrderID, DispatchID: record.ID,
		VehicleID: record.VehicleID, Type: biz.OrderDispatchEventAttemptPending, OccurredAt: record.DispatchedAt.UTC()}
	eventPayload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal order dispatch event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.dispatch_outbox (id,dispatch_id,idempotency_key,payload,status)
		VALUES ($1,$2,$3,$4,0)`, repository.data.dispatchIDGenerator.New(), record.ID, record.DispatchNo, instructionPayload); err != nil {
		return fmt.Errorf("insert vehicle dispatch outbox: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.order_dispatch_outbox (id,dispatch_id,idempotency_key,event_type,payload,status)
		VALUES ($1,$2,$3,$4,$5,0)`, repository.data.dispatchIDGenerator.New(), record.ID, record.DispatchNo, event.Type, eventPayload); err != nil {
		return fmt.Errorf("insert order dispatch outbox: %w", err)
	}
	return nil
}

func countDispatchAttempts(ctx context.Context, tx pgx.Tx, orderID int64) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM public.dispatch_records WHERE order_id = $1", orderID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count dispatch attempts: %w", err)
	}
	return count, nil
}

func selectNearestCandidate(ctx context.Context, tx pgx.Tx, snapshot biz.OrderDispatchSnapshot, speedKMH float64) (biz.CandidateVehicle, error) {
	rows, err := tx.Query(ctx, `SELECT vehicle.id, latest.current_longitude, latest.current_latitude
		FROM public.vehicles vehicle JOIN LATERAL (
			SELECT online_status,operation_status,fault_code,is_accepting_dispatch,current_longitude,current_latitude
			FROM public.vehicle_status_logs WHERE vehicle_id=vehicle.id ORDER BY reported_at DESC,id DESC LIMIT 1
		) latest ON TRUE
		WHERE vehicle.service_area_id=$1 AND latest.online_status=1 AND latest.operation_status=1
		AND latest.is_accepting_dispatch AND (latest.fault_code IS NULL OR btrim(latest.fault_code)='')
		-- 同一订单的历史候选车不能再次参与补派。
		AND NOT EXISTS (SELECT 1 FROM public.dispatch_records previous
			WHERE previous.order_id=$2 AND previous.vehicle_id=vehicle.id)
		AND latest.current_longitude IS NOT NULL AND latest.current_latitude IS NOT NULL`, snapshot.ServiceAreaID, snapshot.OrderID)
	if err != nil {
		return biz.CandidateVehicle{}, fmt.Errorf("list dispatch candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]biz.CandidateVehicle, 0)
	for rows.Next() {
		var id int64
		var longitude, latitude float64
		if err := rows.Scan(&id, &longitude, &latitude); err != nil {
			return biz.CandidateVehicle{}, fmt.Errorf("scan dispatch candidate: %w", err)
		}
		candidate, err := biz.MeasureCandidate(id, biz.GeoPoint{Longitude: longitude, Latitude: latitude}, biz.GeoPoint{Longitude: snapshot.Pickup.Longitude, Latitude: snapshot.Pickup.Latitude}, speedKMH)
		if err != nil {
			return biz.CandidateVehicle{}, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return biz.CandidateVehicle{}, fmt.Errorf("iterate dispatch candidates: %w", err)
	}
	if len(candidates) == 0 {
		return biz.CandidateVehicle{}, biz.ErrNoEligibleVehicle
	}
	// 按接驾距离、车辆 ID 的确定性顺序选首辆，避免数据库返回顺序影响派单结果。
	biz.SortCandidates(candidates)
	return candidates[0], nil
}

func timePointer(value time.Time) *time.Time { return &value }
