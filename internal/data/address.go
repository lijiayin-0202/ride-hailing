// 模块1 用户账号与个人中心（成员A）
// 常用地址 repo：passenger_addresses。
// 所有查询强制带 passenger_id 条件，保证乘客维度的数据隔离（AGENT.md §10.3）。
package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// addressRepo 常用地址数据访问实现（实现 biz.AddressRepo）。
type addressRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志。
	log *log.Helper
}

// 默认地址清理 SQL：把同乘客同类型的旧默认项取消。
const (
	// clearDefaultOnCreate 新增地址前置为默认时，先清掉同类型旧默认。
	clearDefaultOnCreate = `UPDATE passenger_addresses SET is_default = false, updated_at = now()
		WHERE passenger_id = $1 AND address_type = $2 AND is_default = true`
	// clearDefaultOnUpdate 修改地址置为默认时，清掉同类型其他记录的默认标记（排除自身）。
	clearDefaultOnUpdate = `UPDATE passenger_addresses SET is_default = false, updated_at = now()
		WHERE passenger_id = $1 AND address_type = $2 AND is_default = true AND id <> $3`
)

// NewAddressRepo 构造函数（wire 注入）。
func NewAddressRepo(d *Data, logger log.Logger) biz.AddressRepo {
	return &addressRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/address")),
	}
}

// addressSelect 统一列清单；station_id 用 coalesce 兜底成 0，
// 经纬度转 float8 便于直接映射到 float64。
const addressSelect = `id, passenger_id, address_type, address_name, address, longitude::float8, latitude::float8,
	coalesce(station_id, 0) AS station_id, is_default, last_used_at, created_at, updated_at`

// scanAddress 统一扫描函数：无记录转领域错误，last_used_at 可空用指针承接。
func scanAddress(row rowScanner) (*biz.Address, error) {
	var (
		a        biz.Address
		lastUsed *time.Time
	)
	err := row.Scan(&a.ID, &a.PassengerID, &a.AddressType, &a.AddressName, &a.Address,
		&a.Longitude, &a.Latitude, &a.StationID, &a.IsDefault, &lastUsed, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		// 不存在（含越权查询）统一按「地址不存在」返回，避免探测资源是否存在。
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, biz.ErrAddressNotFound
		}
		return nil, err
	}
	a.LastUsedAt = lastUsed
	return &a, nil
}

// List 分页查询，默认地址优先、更新时间倒序。
func (r *addressRepo) List(ctx context.Context, passengerID int64, addressType *int32, offset, limit int32) ([]*biz.Address, int64, error) {
	// 基础条件：只查本人地址。
	args := []any{passengerID}
	where := "passenger_id = $1"

	// 地址类型筛选为可选条件，保持参数绑定。
	if addressType != nil {
		args = append(args, *addressType)
		where += fmt.Sprintf(" AND address_type = $%d", len(args))
	}

	// 先取总数供前端分页。
	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM passenger_addresses WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// 无数据返回空切片。
	if total == 0 {
		return []*biz.Address{}, 0, nil
	}

	// 追加分页参数并拼装查询（占位符按 args 长度递增）。
	args = append(args, limit, offset)
	query := `SELECT ` + addressSelect + ` FROM passenger_addresses WHERE ` + where +
		fmt.Sprintf(` ORDER BY is_default DESC, updated_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.data.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	// 游标必须关闭。
	defer rows.Close()

	items := make([]*biz.Address, 0, limit)
	for rows.Next() {
		item, err := scanAddress(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	// 遍历结束检查 Err。
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get 按 id + passenger_id 查询，越权访问按不存在处理。
func (r *addressRepo) Get(ctx context.Context, id, passengerID int64) (*biz.Address, error) {
	row := r.data.db.QueryRow(ctx,
		`SELECT `+addressSelect+` FROM passenger_addresses WHERE id = $1 AND passenger_id = $2`, id, passengerID)
	return scanAddress(row)
}

// Create 新增地址；默认项唯一由事务内清理 + 数据库部分唯一索引双重保证。
func (r *addressRepo) Create(ctx context.Context, a *biz.Address) error {
	// 事务：先清旧默认，再插新记录，两步必须原子。
	tx, err := r.data.db.Begin(ctx)
	if err != nil {
		return err
	}
	// 提前返回时回滚。
	defer func() { _ = tx.Rollback(ctx) }()

	// 新增的是默认地址时，先取消同类型旧默认。
	if a.IsDefault {
		if _, err := tx.Exec(ctx, clearDefaultOnCreate, a.PassengerID, a.AddressType); err != nil {
			return err
		}
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO passenger_addresses
			(passenger_id, address_type, address_name, address, longitude, latitude, station_id, is_default, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id`,
		a.PassengerID, a.AddressType, a.AddressName, a.Address, a.Longitude, a.Latitude,
		nullableInt64(a.StationID), a.IsDefault, a.CreatedAt, a.UpdatedAt).Scan(&a.ID)
	if err != nil {
		// 并发置默认触发部分唯一索引冲突 → 业务冲突，提示前端重试。
		if isUniqueViolation(err) {
			return biz.ErrBusinessConflict
		}
		return err
	}
	return tx.Commit(ctx)
}

// Update 修改地址；WHERE 带 passenger_id，保证只能改自己的记录。
func (r *addressRepo) Update(ctx context.Context, a *biz.Address) error {
	tx, err := r.data.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 置为默认时，先清掉同类型其他记录的默认标记。
	if a.IsDefault {
		if _, err := tx.Exec(ctx, clearDefaultOnUpdate, a.PassengerID, a.AddressType, a.ID); err != nil {
			return err
		}
	}

	tag, err := tx.Exec(ctx,
		`UPDATE passenger_addresses
		 SET address_type = $3, address_name = $4, address = $5, longitude = $6, latitude = $7,
		     station_id = $8, is_default = $9, updated_at = $10
		 WHERE id = $1 AND passenger_id = $2`,
		a.ID, a.PassengerID, a.AddressType, a.AddressName, a.Address, a.Longitude, a.Latitude,
		nullableInt64(a.StationID), a.IsDefault, a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return biz.ErrBusinessConflict
		}
		return err
	}
	// 影响 0 行说明记录不存在或非本人，按不存在返回。
	if tag.RowsAffected() == 0 {
		return biz.ErrAddressNotFound
	}
	return tx.Commit(ctx)
}

// Delete 删除地址。
// SQL 同时带 id 与 passenger_id 条件，避免越权删除他人地址（AGENT.md §8.2、§10.3）。
func (r *addressRepo) Delete(ctx context.Context, id, passengerID int64) (int64, error) {
	tag, err := r.data.db.Exec(ctx,
		`DELETE FROM passenger_addresses WHERE id = $1 AND passenger_id = $2`, id, passengerID)
	if err != nil {
		return 0, err
	}
	// 返回影响行数，供业务层判定"不存在/越权"。
	return tag.RowsAffected(), nil
}

// nullableInt64 0 值按 NULL 处理，避免 station_id 外键语义出现 0 号站点。
func nullableInt64(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}
