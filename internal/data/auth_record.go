// 模块1 用户账号与个人中心（成员A）
// 登录审计 repo：passenger_auth_records。
// 只保存脱敏手机号、结果与来源信息，不落验证码/令牌明文（AGENT.md §10.2）。
package data

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// passengerAuthRecordRepo 登录审计数据访问实现（实现 biz.PassengerAuthRecordRepo）。
type passengerAuthRecordRepo struct {
	// data 持有连接池等基础设施。
	data *Data
	// log 结构化日志。
	log *log.Helper
}

// NewPassengerAuthRecordRepo 构造函数（wire 注入）。
func NewPassengerAuthRecordRepo(d *Data, logger log.Logger) biz.PassengerAuthRecordRepo {
	return &passengerAuthRecordRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/auth_record")),
	}
}

// Create 写入登录审计记录。passenger_id 允许为空（新号码校验失败时尚未建档）。
func (r *passengerAuthRecordRepo) Create(ctx context.Context, record *biz.PassengerAuthRecord) error {
	// 单条 INSERT，全部参数绑定；失败由业务层决定是否阻断（登录审计失败不阻断登录）。
	_, err := r.data.db.Exec(ctx,
		`INSERT INTO passenger_auth_records
			(passenger_id, mobile_masked, auth_type, login_result, failure_reason,
			 device_id, device_name, device_model, client_ip, login_location, user_agent, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		record.PassengerID, record.Mobile, record.AuthType, record.LoginResult,
		record.FailureReason, record.DeviceID, record.DeviceName, record.DeviceModel,
		record.ClientIP, record.LoginLocation, record.UserAgent, record.CreatedAt)
	return err
}
