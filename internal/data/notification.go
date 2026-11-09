// 模块1 用户账号与个人中心（成员A）
// 消息中心 repo：notifications。
// 已读操作必须带 passenger_id 条件，避免越权修改他人消息（AGENT.md §8.2：UPDATE 必带 WHERE）。
package data

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// notificationRepo 消息数据访问实现（实现 biz.NotificationRepo）。
type notificationRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志。
	log *log.Helper
}

// NewNotificationRepo 构造函数（wire 注入）。
func NewNotificationRepo(d *Data, logger log.Logger) biz.NotificationRepo {
	return &notificationRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/notification")),
	}
}

// notificationSelect 统一列清单；content 可能为空，用 coalesce 兜底避免扫描 NULL 失败。
const notificationSelect = `id, notification_type, title, coalesce(content, '') AS content,
	related_order_id, is_read, read_at, created_at`

// List 分页查询，按创建时间倒序（最新消息在前）。
func (r *notificationRepo) List(ctx context.Context, passengerID int64, isRead *bool, offset, limit int32) ([]*biz.Notification, int64, error) {
	// 基础条件：只查本人消息，保证用户数据隔离。
	args := []any{passengerID}
	where := "passenger_id = $1"

	// 已读状态筛选为可选条件，仍保持参数绑定。
	if isRead != nil {
		args = append(args, *isRead)
		where += fmt.Sprintf(" AND is_read = $%d", len(args))
	}

	// 先取总数供前端分页（列表接口强制分页，AGENT.md §9.3）。
	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// 无数据返回空切片而非 nil，前端可直接遍历。
	if total == 0 {
		return []*biz.Notification{}, 0, nil
	}

	// 追加分页参数并拼装查询（占位符按 args 长度递增，避免拼接值）。
	args = append(args, limit, offset)
	query := `SELECT ` + notificationSelect + ` FROM notifications WHERE ` + where +
		fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.data.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	// 游标必须关闭，保证连接归还。
	defer rows.Close()

	items := make([]*biz.Notification, 0, limit)
	for rows.Next() {
		var n biz.Notification
		if err := rows.Scan(&n.ID, &n.NotificationType, &n.Title, &n.Content, &n.RelatedOrderID,
			&n.IsRead, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, &n)
	}
	// 遍历结束必须检查 Err，避免静默丢错。
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// CountUnread 未读消息数（个人中心聚合用）。
func (r *notificationRepo) CountUnread(ctx context.Context, passengerID int64) (int32, error) {
	var count int32
	err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE passenger_id = $1 AND is_read = false`, passengerID).Scan(&count)
	return count, err
}

// MarkRead 按 id 列表标记已读（只处理本人且未读的记录）。
func (r *notificationRepo) MarkRead(ctx context.Context, passengerID int64, ids []int64) (int32, error) {
	// 空列表直接返回，避免发出无意义的 UPDATE。
	if len(ids) == 0 {
		return 0, nil
	}
	// WHERE 同时限定 passenger_id 与 is_read，保证只能改自己且幂等。
	tag, err := r.data.db.Exec(ctx,
		`UPDATE notifications SET is_read = true, read_at = now()
		 WHERE passenger_id = $1 AND id = ANY($2) AND is_read = false`, passengerID, ids)
	if err != nil {
		return 0, err
	}
	// 返回实际影响行数，供接口回显 updated_count。
	return int32(tag.RowsAffected()), nil
}

// MarkAllRead 全部标记已读（mark_all=true 时使用）。
func (r *notificationRepo) MarkAllRead(ctx context.Context, passengerID int64) (int32, error) {
	tag, err := r.data.db.Exec(ctx,
		`UPDATE notifications SET is_read = true, read_at = now()
		 WHERE passenger_id = $1 AND is_read = false`, passengerID)
	if err != nil {
		return 0, err
	}
	return int32(tag.RowsAffected()), nil
}
