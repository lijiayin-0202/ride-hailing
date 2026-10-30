// 模块1 用户账号与个人中心（成员A）
// 乘客账户 repo：passengers / passenger_wallets。
// 所有 SQL 均为参数绑定，禁止字符串拼接用户输入（AGENT.md §8.3）。
package data

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// passengerRepo 乘客账户数据访问实现（实现 biz.PassengerRepo）。
type passengerRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志；手机号等敏感字段必须脱敏后再打印。
	log *log.Helper
}

// NewPassengerRepo 构造函数（wire 注入）。
func NewPassengerRepo(d *Data, logger log.Logger) biz.PassengerRepo {
	return &passengerRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/passenger")),
	}
}

// passengerSelect 统一列清单；可空列用 coalesce 兜底，避免扫描 NULL 失败。
const passengerSelect = `id, passenger_no, mobile, coalesce(nickname,'') AS nickname,
	coalesce(avatar_url,'') AS avatar_url, real_name_status, account_status, point_balance,
	coalesce(preferences,'{}'::jsonb) AS preferences, last_login_at, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPassenger(row rowScanner) (*biz.Passenger, error) {
	var (
		p         biz.Passenger
		prefBytes []byte
		lastLogin *time.Time
	)
	err := row.Scan(&p.ID, &p.PassengerNo, &p.Mobile, &p.Nickname, &p.AvatarURL,
		&p.RealNameStatus, &p.AccountStatus, &p.PointBalance, &prefBytes, &lastLogin, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, biz.ErrPassengerNotFound
		}
		return nil, err
	}
	p.LastLoginAt = lastLogin
	if len(prefBytes) > 0 {
		// 偏好字段历史数据可能不完整，解析失败不影响主流程。
		_ = json.Unmarshal(prefBytes, &p.Preferences)
	}
	return &p, nil
}

// GetByMobile 按手机号查询。
// 手机号唯一索引 uk_passengers_mobile 保证等值查询命中至多一条。
// 说明：这里不过滤任何账号状态——手机号是全局唯一索引，若把注销账号当作「不存在」，
// 登录会走自动注册并撞唯一键，导致注销过的手机号永远无法登录也无法重新注册；
// 账号状态由业务层判断并给出明确提示（冻结/注销）。
func (r *passengerRepo) GetByMobile(ctx context.Context, mobile string) (*biz.Passenger, error) {
	row := r.data.db.QueryRow(ctx,
		`SELECT `+passengerSelect+` FROM passengers WHERE mobile = $1`, mobile)
	return scanPassenger(row)
}

// GetByID 按主键查询。
func (r *passengerRepo) GetByID(ctx context.Context, id int64) (*biz.Passenger, error) {
	row := r.data.db.QueryRow(ctx,
		`SELECT `+passengerSelect+` FROM passengers WHERE id = $1`, id)
	return scanPassenger(row)
}

// Create 创建乘客并初始化钱包（同一事务，保证账户与钱包一致）。
func (r *passengerRepo) Create(ctx context.Context, p *biz.Passenger) error {
	// 偏好以 JSONB 落库，序列化失败直接返回（数据结构错误，重试无意义）。
	prefBytes, err := json.Marshal(p.Preferences)
	if err != nil {
		return err
	}

	// 事务：账户与钱包必须同成同败，否则会出现"有账号无钱包"的脏数据。
	tx, err := r.data.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = tx.QueryRow(ctx,
		`INSERT INTO passengers
			(passenger_no, mobile, nickname, avatar_url, real_name_status, account_status, point_balance, preferences, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
		 RETURNING id`,
		p.PassengerNo, p.Mobile, p.Nickname, p.AvatarURL, p.RealNameStatus, p.AccountStatus,
		p.PointBalance, string(prefBytes), p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
	if err != nil {
		// 唯一索引冲突（手机号并发注册）交由业务层重查，不在此静默吞掉。
		if isUniqueViolation(err) {
			return biz.ErrBusinessConflict
		}
		return err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO passenger_wallets (passenger_id, balance, frozen_amount, version, created_at, updated_at)
		 VALUES ($1, 0, 0, 0, now(), now())`, p.ID); err != nil {
		if isUniqueViolation(err) {
			// 钱包已存在说明账户重复创建，整体回滚。
			return biz.ErrBusinessConflict
		}
		return err
	}

	return tx.Commit(ctx)
}

// UpdateLastLogin 更新最后登录时间。
func (r *passengerRepo) UpdateLastLogin(ctx context.Context, id int64, at time.Time) error {
	_, err := r.data.db.Exec(ctx,
		`UPDATE passengers SET last_login_at = $2, updated_at = now() WHERE id = $1`, id, at)
	return err
}

// UpdatePreferences 全量覆盖偏好设置。
func (r *passengerRepo) UpdatePreferences(ctx context.Context, id int64, pref biz.PassengerPreferences) error {
	prefBytes, err := json.Marshal(pref)
	if err != nil {
		return err
	}
	tag, err := r.data.db.Exec(ctx,
		`UPDATE passengers SET preferences = $2::jsonb, updated_at = now() WHERE id = $1`, id, string(prefBytes))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return biz.ErrPassengerNotFound
	}
	return nil
}

// UpdateNickname 修改用户名（昵称）。
func (r *passengerRepo) UpdateNickname(ctx context.Context, id int64, nickname string) error {
	tag, err := r.data.db.Exec(ctx,
		`UPDATE passengers SET nickname = $2, updated_at = now() WHERE id = $1`, id, nickname)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return biz.ErrPassengerNotFound
	}
	return nil
}

// UpdateMobile 更换绑定手机号。
// 手机号上有全局唯一索引，并发换绑同一号码时由数据库兜底，这里转换成业务冲突。
func (r *passengerRepo) UpdateMobile(ctx context.Context, id int64, mobile string) error {
	tag, err := r.data.db.Exec(ctx,
		`UPDATE passengers SET mobile = $2, updated_at = now() WHERE id = $1`, id, mobile)
	if err != nil {
		if isUniqueViolation(err) {
			return biz.ErrBusinessConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return biz.ErrPassengerNotFound
	}
	return nil
}

// WalletBalance 钱包余额（元）。钱包未初始化时按 0 处理。
func (r *passengerRepo) WalletBalance(ctx context.Context, id int64) (float64, error) {
	var balance float64
	err := r.data.db.QueryRow(ctx,
		`SELECT coalesce(balance, 0)::float8 FROM passenger_wallets WHERE passenger_id = $1`, id).Scan(&balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return balance, nil
}

// isUniqueViolation 判断 PostgreSQL 唯一约束冲突（SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
