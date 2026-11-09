package data

import (
	"context"
	"time"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

const commandColumns = `command_no, vehicle_id, COALESCE(related_order_id, 0), command_type, command_payload::text, priority, status, issued_at, acknowledged_at, finished_at, COALESCE(result_message, '')`

// ListPendingCommands 将待发送指令标记为已发送，并返回尚未确认的指令。
func (r *VehicleHMIRepo) ListPendingCommands(ctx context.Context, vehicleID int64, commandNo string) ([]*biz.VehicleCommand, error) {
	pool, err := r.pool()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const markSent = `UPDATE robotaxi.vehicle_commands SET status = 1 WHERE vehicle_id = $1 AND status = 0 AND ($2 = '' OR command_no = $2)`
	if _, err = tx.Exec(ctx, markSent, vehicleID, commandNo); err != nil {
		return nil, err
	}
	query := `SELECT ` + commandColumns + ` FROM robotaxi.vehicle_commands WHERE vehicle_id = $1 AND status = 1 AND ($2 = '' OR command_no = $2) ORDER BY priority ASC, issued_at ASC`
	rows, err := tx.Query(ctx, query, vehicleID, commandNo)
	if err != nil {
		return nil, err
	}
	commands, err := scanCommands(rows)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return commands, nil
}

// ListActiveCommands 只读查询待发送和已发送指令，不推进指令状态。
func (r *VehicleHMIRepo) ListActiveCommands(ctx context.Context, vehicleID int64, commandNo string) ([]*biz.VehicleCommand, error) {
	pool, err := r.pool()
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + commandColumns + ` FROM robotaxi.vehicle_commands WHERE vehicle_id = $1 AND status IN (0, 1) AND ($2 = '' OR command_no = $2) ORDER BY priority ASC, issued_at ASC`
	rows, err := pool.Query(ctx, query, vehicleID, commandNo)
	if err != nil {
		return nil, err
	}
	return scanCommands(rows)
}

// GetCommand 按指令编号查询最新指令状态。
func (r *VehicleHMIRepo) GetCommand(ctx context.Context, commandNo string) (*biz.VehicleCommand, error) {
	pool, err := r.pool()
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + commandColumns + ` FROM robotaxi.vehicle_commands WHERE command_no = $1`
	return scanCommand(pool.QueryRow(ctx, query, commandNo))
}

// UpdateCommandStatus 使用预期状态条件更新，防止重复或乱序请求覆盖数据。
func (r *VehicleHMIRepo) UpdateCommandStatus(ctx context.Context, commandNo string, expected, target biz.CommandStatus, result string, changedAt time.Time) (*biz.VehicleCommand, error) {
	pool, err := r.pool()
	if err != nil {
		return nil, err
	}
	query := `UPDATE robotaxi.vehicle_commands SET status = $1, acknowledged_at = CASE WHEN $1 = 2 THEN $2 ELSE acknowledged_at END, finished_at = CASE WHEN $1 IN (3, 4) THEN $2 ELSE finished_at END, result_message = CASE WHEN $1 IN (3, 4) THEN $3 ELSE result_message END WHERE command_no = $4 AND status = $5 RETURNING ` + commandColumns
	return scanCommand(pool.QueryRow(ctx, query, target, changedAt, result, commandNo, expected))
}
