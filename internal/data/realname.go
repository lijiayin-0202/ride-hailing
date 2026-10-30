// 模块1 用户账号与个人中心（成员A）
// 实名认证 repo：passenger_real_name_records（同步 passengers.real_name_status）。
// 所有 SQL 均为参数绑定，禁止字符串拼接用户输入（AGENT.md §8.3）。
package data

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type realNameRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志；只允许打印掩码，禁止打印证件原文。
	log *log.Helper
}

// NewRealNameRepo 构造函数（wire 注入）。
func NewRealNameRepo(d *Data, logger log.Logger) biz.RealNameRepo {
	return &realNameRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/realname")),
	}
}

const realNameSelect = `id, passenger_id, real_name_masked, id_card_masked, id_card_hash, status,
	coalesce(reject_reason,'') AS reject_reason, submitted_at, verified_at, created_at, updated_at`

// GetByPassenger 查询该乘客最近一次认证记录；从未提交返回 (nil, nil)。
func (r *realNameRepo) GetByPassenger(ctx context.Context, passengerID int64) (*biz.RealNameRecord, error) {
	row := r.data.db.QueryRow(ctx,
		`SELECT `+realNameSelect+` FROM passenger_real_name_records WHERE passenger_id = $1`, passengerID)

	var (
		record     biz.RealNameRecord
		verifiedAt *time.Time
	)
	err := row.Scan(&record.ID, &record.PassengerID, &record.RealNameMasked, &record.IDCardMasked,
		&record.IDCardHash, &record.Status, &record.RejectReason, &record.SubmittedAt,
		&verifiedAt, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	record.VerifiedAt = verifiedAt
	return &record, nil
}

// IDCardBound 该身份证是否已被其他乘客占用（认证中/已认证记录参与判重）。
func (r *realNameRepo) IDCardBound(ctx context.Context, idCardHash string, excludePassengerID int64) (bool, error) {
	var exists bool
	err := r.data.db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM passenger_real_name_records
			 WHERE id_card_hash = $1 AND passenger_id <> $2 AND status IN ($3, $4)
		 )`,
		idCardHash, excludePassengerID, biz.RealNameStatusPending, biz.RealNameStatusVerified).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// Submit 覆盖式写入认证记录，并把 passengers.real_name_status 置为「认证中」（同一事务）。
// 覆盖语义：同一乘客只保留一条记录，重新提交会重置状态、清空驳回原因与审核时间。
func (r *realNameRepo) Submit(ctx context.Context, record *biz.RealNameRecord) (*biz.RealNameRecord, error) {
	// 事务：认证记录与乘客实名状态必须同成同败，否则会出现「有记录无状态」的脏数据。
	tx, err := r.data.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 覆盖式写入：乘客已有记录时更新（重置状态、清空驳回原因与审核时间）。
	err = tx.QueryRow(ctx,
		`INSERT INTO passenger_real_name_records
			(passenger_id, real_name_masked, id_card_masked, id_card_hash, status, reject_reason,
			 submitted_at, verified_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, '', now(), NULL, now(), now())
		 ON CONFLICT (passenger_id) DO UPDATE SET
			real_name_masked = EXCLUDED.real_name_masked,
			id_card_masked   = EXCLUDED.id_card_masked,
			id_card_hash     = EXCLUDED.id_card_hash,
			status           = EXCLUDED.status,
			reject_reason    = '',
			submitted_at     = now(),
			verified_at      = NULL,
			updated_at       = now()
		 RETURNING id, submitted_at, created_at, updated_at`,
		record.PassengerID, record.RealNameMasked, record.IDCardMasked, record.IDCardHash, record.Status,
	).Scan(&record.ID, &record.SubmittedAt, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		// 证件哈希唯一索引冲突（同一证件被并发绑定）按业务冲突返回。
		if isUniqueViolation(err) {
			return nil, biz.ErrBusinessConflict
		}
		return nil, err
	}

	// 同步乘客维度的实名状态（个人中心/摘要接口读这里），提交后统一进入「认证中」。
	tag, err := tx.Exec(ctx,
		`UPDATE passengers SET real_name_status = $2, updated_at = now() WHERE id = $1`,
		record.PassengerID, biz.RealNameStatusPending)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, biz.ErrPassengerNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	record.VerifiedAt = nil
	return record, nil
}
