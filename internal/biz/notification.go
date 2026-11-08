// 模块1 用户账号与个人中心（成员A）
// 消息中心：列表、未读数、已读标记。
// 数据表：notifications（消息体产生方为模块5，本模块只做乘客侧读取与已读状态维护）。
package biz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// 消息类型（notifications.notification_type）
const (
	NotificationTypeOrder   int32 = 1 // 订单
	NotificationTypePayment int32 = 2 // 支付
	NotificationTypePromo   int32 = 3 // 活动
	NotificationTypeSafety  int32 = 4 // 安全
	NotificationTypeSystem  int32 = 5 // 系统
)

// 单次已读标记的最大条数，防止超大请求。
const maxMarkReadIDs = 100

// Notification 消息实体（notifications）。
type Notification struct {
	// ID 消息主键。
	ID int64
	// NotificationType 类型：1 订单 / 2 支付 / 3 活动 / 4 安全 / 5 系统。
	NotificationType int32
	// Title 标题（≤120 字符）。
	Title string
	// Content 正文（≤1000 字符）。
	Content string
	// RelatedOrderID 关联订单；nil 表示系统/活动类消息。
	RelatedOrderID *int64
	// IsRead 是否已读。
	IsRead bool
	// ReadAt 已读时间；未读为 nil。
	ReadAt *time.Time
	// CreatedAt 创建时间（由模块5 生产时写入）。
	CreatedAt time.Time
}

// NotificationListResult 分页结果。
type NotificationListResult struct {
	// Items 当前页消息列表。
	Items []*Notification
	// Total 满足筛选条件的总数。
	Total int64
	// Page 归一化后的页码。
	Page int32
	// PageSize 归一化后的每页条数。
	PageSize int32
	// UnreadCount 未读总数（与分页无关）。
	UnreadCount int32
}

// NotificationRepo 消息数据访问接口。
type NotificationRepo interface {
	List(ctx context.Context, passengerID int64, isRead *bool, offset, limit int32) ([]*Notification, int64, error)
	CountUnread(ctx context.Context, passengerID int64) (int32, error)
	// MarkRead 按 id 列表标记已读，返回更新条数；只影响当前乘客的消息。
	MarkRead(ctx context.Context, passengerID int64, ids []int64) (int32, error)
	// MarkAllRead 全部标记已读，返回更新条数。
	MarkAllRead(ctx context.Context, passengerID int64) (int32, error)
}

// NotificationUsecase 消息中心用例。
type NotificationUsecase struct {
	repo NotificationRepo
	log  *log.Helper
}

// NewNotificationUsecase 构造函数（wire 注入）。
func NewNotificationUsecase(repo NotificationRepo, logger log.Logger) *NotificationUsecase {
	return &NotificationUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "biz/notification")),
	}
}

// List 分页查询消息，返回未读总数。
func (uc *NotificationUsecase) List(ctx context.Context, passengerID int64, page, pageSize int32, isRead *bool) (*NotificationListResult, error) {
	page, pageSize, offset := NormalizePage(page, pageSize)
	items, total, err := uc.repo.List(ctx, passengerID, isRead, offset, pageSize)
	if err != nil {
		return nil, err
	}
	unread, err := uc.repo.CountUnread(ctx, passengerID)
	if err != nil {
		uc.log.WithContext(ctx).Warnf("统计未读消息失败 passenger_id=%d err=%v", passengerID, err)
	}
	return &NotificationListResult{Items: items, Total: total, Page: page, PageSize: pageSize, UnreadCount: unread}, nil
}

// MarkRead 标记已读。ids 为空且 markAll 为真时全部已读。
func (uc *NotificationUsecase) MarkRead(ctx context.Context, passengerID int64, ids []int64, markAll bool) (int32, int32, error) {
	// 既没给 ids 也没要求全部已读：无法确定目标，直接拒绝。
	if !markAll && len(ids) == 0 {
		return 0, 0, invalidArgument("请指定消息ID或选择全部已读")
	}
	// 单次上限 100 条，防止超大请求拖垮数据库。
	if len(ids) > maxMarkReadIDs {
		return 0, 0, invalidArgument("单次最多标记 %d 条消息", maxMarkReadIDs)
	}

	var (
		updated int32
		err     error
	)
	// 两种模式互斥：mark_all=true 走全量，否则按 id 列表。
	if markAll {
		updated, err = uc.repo.MarkAllRead(ctx, passengerID)
	} else {
		updated, err = uc.repo.MarkRead(ctx, passengerID, ids)
	}
	if err != nil {
		return 0, 0, err
	}
	unread, err := uc.repo.CountUnread(ctx, passengerID)
	if err != nil {
		uc.log.WithContext(ctx).Warnf("统计未读消息失败 passenger_id=%d err=%v", passengerID, err)
	}
	return updated, unread, nil
}
