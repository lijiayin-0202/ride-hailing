// 模块1 用户账号与个人中心（成员A）
// 优惠券 repo：passenger_coupons（持券实例）联表 coupons（券模板）。
// 约定：所有 SQL 参数绑定，禁止字符串拼接用户输入（AGENT.md §8.3）。
package data

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// couponRepo 持券实例数据访问实现（实现 biz.CouponRepo）。
type couponRepo struct {
	// data 持有连接池等基础设施，由 NewData 统一初始化。
	data *Data
	// log 结构化日志，禁止打印证件/手机号明文。
	log *log.Helper
}

// NewCouponRepo 构造函数（wire 注入）。
func NewCouponRepo(d *Data, logger log.Logger) biz.CouponRepo {
	return &couponRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/coupon")),
	}
}

// couponSelect 统一列清单：券实例字段取自 passenger_coupons（pc），
// 券模板字段取自 coupons（c）；金额/折扣用 coalesce 兜底，避免扫描 NULL 失败。
const couponSelect = `pc.id, pc.coupon_id, pc.coupon_instance_no, c.coupon_name, c.coupon_type,
	coalesce(c.discount_amount, 0)::float8 AS discount_amount,
	coalesce(c.discount_rate, 0)::float8 AS discount_rate,
	coalesce(c.minimum_order_amount, 0)::float8 AS minimum_order_amount,
	c.service_area_id, pc.received_at, pc.expires_at, pc.status, pc.received_at, pc.expires_at`

// ListByPassenger 分页查询持券实例，按到期时间升序（临期在前）。
func (r *couponRepo) ListByPassenger(ctx context.Context, passengerID int64, status *int32, offset, limit int32) ([]*biz.Coupon, int64, error) {
	// 基础过滤条件：只查本人券，保证用户数据隔离。
	args := []any{passengerID}
	where := "pc.passenger_id = $1"

	// 状态筛选为可选条件：按需追加占位符，仍保持参数绑定。
	if status != nil {
		args = append(args, *status)
		where += fmt.Sprintf(" AND pc.status = $%d", len(args))
	}

	// 先查总数：列表接口必须返回 total 供前端分页（AGENT.md §9.3）。
	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM passenger_coupons pc WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 空结果直接返回空切片（而非 nil），前端可安全遍历。
	if total == 0 {
		return []*biz.Coupon{}, 0, nil
	}

	// 追加分页参数：limit/offset 追加在状态参数之后。
	args = append(args, limit, offset)
	query := `SELECT ` + couponSelect + `
		FROM passenger_coupons pc
		JOIN coupons c ON c.id = pc.coupon_id
		WHERE ` + where + fmt.Sprintf(` ORDER BY pc.expires_at ASC, pc.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.data.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	// 游标必须关闭，否则连接不会归还连接池。
	defer rows.Close()

	items := make([]*biz.Coupon, 0, limit)
	for rows.Next() {
		var (
			c biz.Coupon
			// areaID 可为 NULL（不限区域），用指针承接后再赋值。
			areaID    *int64
			validFrom time.Time
			validTo   time.Time
		)
		if err := rows.Scan(&c.ID, &c.CouponID, &c.CouponInstanceNo, &c.CouponName, &c.CouponType,
			&c.DiscountAmount, &c.DiscountRate, &c.MinimumOrderAmount, &areaID,
			&validFrom, &validTo, &c.Status, &c.ReceivedAt, &c.ExpiresAt); err != nil {
			return nil, 0, err
		}
		// NULL 区域映射为 nil，业务层可据此判断"不限区域"。
		c.ServiceAreaID = areaID
		c.ValidFrom = validFrom
		c.ValidTo = validTo
		items = append(items, &c)
	}
	// 遍历结束后必须检查 Err，避免静默丢失读取错误。
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CountAvailable 统计可用券：未使用且未过期（个人中心聚合用）。
func (r *couponRepo) CountAvailable(ctx context.Context, passengerID int64, now time.Time) (int32, error) {
	var count int32
	err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM passenger_coupons
		 WHERE passenger_id = $1 AND status = $2 AND expires_at > $3`,
		passengerID, biz.CouponStatusUnused, now).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
