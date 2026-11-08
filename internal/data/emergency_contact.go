// 模块1 用户账号与个人中心（成员A）
// 紧急联系人 repo：passenger_emergency_contacts。
// 所有读写强制带 passenger_id 条件，保证乘客维度数据隔离（AGENT.md §10.3）。
package data

import (
	"context"
	"errors"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// emergencyContactRepo 紧急联系人数据访问实现（实现 biz.EmergencyContactRepo）。
type emergencyContactRepo struct {
	data *Data
	log  *log.Helper
}

// NewEmergencyContactRepo 构造函数（wire 注入）。
func NewEmergencyContactRepo(d *Data, logger log.Logger) biz.EmergencyContactRepo {
	return &emergencyContactRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/emergency_contact")),
	}
}

// emergencyContactSelect 统一列清单。
const emergencyContactSelect = `id, passenger_id, contact_name, contact_mobile, relation, created_at, updated_at`

// Get 按 passenger_id 查询；未维护返回 (nil, nil)，避免上层把「没有」当成错误。
func (r *emergencyContactRepo) Get(ctx context.Context, passengerID int64) (*biz.EmergencyContact, error) {
	var contact biz.EmergencyContact
	err := r.data.db.QueryRow(ctx,
		`SELECT `+emergencyContactSelect+` FROM passenger_emergency_contacts WHERE passenger_id = $1`,
		passengerID).
		Scan(&contact.ID, &contact.PassengerID, &contact.ContactName, &contact.ContactMobile,
			&contact.Relation, &contact.CreatedAt, &contact.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &contact, nil
}

// Save 覆盖写入：唯一约束 passenger_id 决定「一人一条」，冲突时更新而不是新增。
func (r *emergencyContactRepo) Save(ctx context.Context, contact *biz.EmergencyContact) error {
	return r.data.db.QueryRow(ctx,
		`INSERT INTO passenger_emergency_contacts
			(passenger_id, contact_name, contact_mobile, relation, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (passenger_id) DO UPDATE
		 SET contact_name = EXCLUDED.contact_name,
		     contact_mobile = EXCLUDED.contact_mobile,
		     relation = EXCLUDED.relation,
		     updated_at = EXCLUDED.updated_at
		 RETURNING id, created_at`,
		contact.PassengerID, contact.ContactName, contact.ContactMobile, contact.Relation,
		contact.CreatedAt, contact.UpdatedAt).
		Scan(&contact.ID, &contact.CreatedAt)
}
