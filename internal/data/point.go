// 模块1 用户账号与个人中心（成员A）
// 积分与签到 repo：point_transactions / sign_in_records / passengers.point_balance。
// 签到采用「唯一约束 + 事务」保证一天一次，重复提交不重复加积分。
package data

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// pointRepo 积分与签到数据访问实现（实现 biz.PointRepo）。
type pointRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志。
	log *log.Helper
}

// NewPointRepo 构造函数（wire 注入）。
func NewPointRepo(d *Data, logger log.Logger) biz.PointRepo {
	return &pointRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/point")),
	}
}

// dateLayout 签到日期统一格式（业务时区自然日，yyyy-MM-dd）。
const dateLayout = "2006-01-02"

// Balance 积分余额（passengers.point_balance 为余额唯一权威来源）。
func (r *pointRepo) Balance(ctx context.Context, passengerID int64) (int32, error) {
	var balance int32
	err := r.data.db.QueryRow(ctx,
		`SELECT point_balance FROM passengers WHERE id = $1`, passengerID).Scan(&balance)
	if err != nil {
		// 乘客不存在转换为领域错误，避免把 pgx 错误透传到接口层。
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, biz.ErrPassengerNotFound
		}
		return 0, err
	}
	return balance, nil
}

// ListTransactions 积分流水分页，按时间倒序。
func (r *pointRepo) ListTransactions(ctx context.Context, passengerID int64, offset, limit int32) ([]*biz.PointTransaction, int64, error) {
	// 先取总数供前端分页。
	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM point_transactions WHERE passenger_id = $1`, passengerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	// 无流水返回空切片，前端可直接遍历。
	if total == 0 {
		return []*biz.PointTransaction{}, 0, nil
	}

	// 分页查询：时间倒序 + id 倒序，保证同一秒内的顺序稳定。
	rows, err := r.data.db.Query(ctx,
		`SELECT id, transaction_no, change_amount, balance_after, source_type, related_order_id,
		        coalesce(remark, '') AS remark, created_at
		 FROM point_transactions
		 WHERE passenger_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT $2 OFFSET $3`, passengerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	// 游标必须关闭。
	defer rows.Close()

	items := make([]*biz.PointTransaction, 0, limit)
	for rows.Next() {
		var t biz.PointTransaction
		if err := rows.Scan(&t.ID, &t.TransactionNo, &t.ChangeAmount, &t.BalanceAfter, &t.SourceType,
			&t.RelatedOrderID, &t.Remark, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, &t)
	}
	// 遍历结束检查 Err。
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// LastSignIn 查询指定自然日的签到记录，不存在返回 (nil, nil)。
func (r *pointRepo) LastSignIn(ctx context.Context, passengerID int64, day time.Time) (*biz.SignInRecord, error) {
	var record biz.SignInRecord
	err := r.data.db.QueryRow(ctx,
		`SELECT id, passenger_id, sign_in_date, reward_points, continuous_days, created_at
		 FROM sign_in_records
		 WHERE passenger_id = $1 AND sign_in_date = $2::date`,
		passengerID, day.Format(dateLayout)).Scan(
		&record.ID, &record.PassengerID, &record.SignInDate, &record.RewardPoints, &record.ContinuousDays, &record.CreatedAt)
	if err != nil {
		// 无记录不是错误：调用方据此判断"今天未签到"。
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// SignIn 事务：写签到记录 → 加积分 → 写流水。
// 依赖 sign_in_records 的 (passenger_id, sign_in_date) 唯一约束实现"一天一次"。
func (r *pointRepo) SignIn(ctx context.Context, passengerID int64, day time.Time, rewardPoints, continuousDays int32, remark string) (*biz.SignInResult, error) {
	// 开启事务：签到记录、余额、流水三者必须同成同败。
	tx, err := r.data.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// 任意提前返回都会回滚，避免脏数据。
	defer func() { _ = tx.Rollback(ctx) }()

	dayStr := day.Format(dateLayout)

	// ON CONFLICT DO NOTHING + RETURNING：冲突时无返回行，用于判定"今天已签到"。
	var recordID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO sign_in_records (passenger_id, sign_in_date, reward_points, continuous_days, created_at)
		 VALUES ($1, $2::date, $3, $4, now())
		 ON CONFLICT (passenger_id, sign_in_date) DO NOTHING
		 RETURNING id`,
		passengerID, dayStr, rewardPoints, continuousDays).Scan(&recordID)

	if errors.Is(err, pgx.ErrNoRows) {
		// 当天已签到：回放已有结果，保证接口幂等（不重复加积分）。
		var (
			existing biz.SignInResult
			date     time.Time
		)
		if err := tx.QueryRow(ctx,
			`SELECT sign_in_date, reward_points, continuous_days FROM sign_in_records
			 WHERE passenger_id = $1 AND sign_in_date = $2::date`, passengerID, dayStr).Scan(
			&date, &existing.RewardPoints, &existing.ContinuousDays); err != nil {
			return nil, err
		}
		// 余额取当前值，便于前端直接刷新积分展示。
		if err := tx.QueryRow(ctx,
			`SELECT point_balance FROM passengers WHERE id = $1`, passengerID).Scan(&existing.PointBalance); err != nil {
			return nil, err
		}
		existing.SignInDate = date
		existing.AlreadySigned = true
		return &existing, tx.Commit(ctx)
	}
	// 其他数据库错误直接上抛，由业务层包装成内部错误。
	if err != nil {
		return nil, err
	}

	// 原子累加积分并返回新余额，避免"先读后写"的并发覆盖。
	var balanceAfter int32
	if err := tx.QueryRow(ctx,
		`UPDATE passengers SET point_balance = point_balance + $2, updated_at = now()
		 WHERE id = $1 RETURNING point_balance`, passengerID, rewardPoints).Scan(&balanceAfter); err != nil {
		// 乘客不存在（理论上不会发生）按领域错误返回。
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, biz.ErrPassengerNotFound
		}
		return nil, err
	}

	// 写积分流水：以流水为对账依据，change_amount 正数表示获得。
	if _, err := tx.Exec(ctx,
		`INSERT INTO point_transactions
			(transaction_no, passenger_id, change_amount, balance_after, source_type, remark, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())`,
		newTransactionNo("PT"), passengerID, rewardPoints, balanceAfter, biz.PointSourceSignIn, remark); err != nil {
		return nil, err
	}

	// 统一提交：三步都成功才落库。
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &biz.SignInResult{
		SignInDate:     day,
		RewardPoints:   rewardPoints,
		ContinuousDays: continuousDays,
		PointBalance:   balanceAfter,
		AlreadySigned:  false,
	}, nil
}

// newTransactionNo 业务流水号：前缀 + 毫秒时间戳 + 3 位随机数，唯一索引兜底。
func newTransactionNo(prefix string) string {
	return fmt.Sprintf("%s%d%03d", prefix, time.Now().UnixMilli(), rand.Intn(1000))
}
