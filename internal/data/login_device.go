// 模块1 用户账号与个人中心（成员A）
// 登录设备与登录记录 repo：passenger_login_devices / passenger_auth_records。
// 所有 SQL 均为参数绑定；查询一律带 passenger_id 条件，保证乘客维度隔离。
package data

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type loginDeviceRepo struct {
	data *Data
	log  *log.Helper
}

// NewLoginDeviceRepo 构造函数（wire 注入）。
func NewLoginDeviceRepo(d *Data, logger log.Logger) biz.LoginDeviceRepo {
	return &loginDeviceRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/login_device")),
	}
}

// Upsert 登记设备：同一乘客同一设备只保留一条，重复登录刷新信息并累加次数。
func (r *loginDeviceRepo) Upsert(ctx context.Context, device *biz.LoginDevice) error {
	_, err := r.data.db.Exec(ctx,
		`INSERT INTO passenger_login_devices
			(passenger_id, device_id, device_name, device_model, os_name, os_version, app_version,
			 last_login_ip, last_login_location, login_count, first_login_at, last_login_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $11, $12)
		 ON CONFLICT (passenger_id, device_id) DO UPDATE SET
			device_name         = EXCLUDED.device_name,
			device_model        = EXCLUDED.device_model,
			os_name             = EXCLUDED.os_name,
			os_version          = EXCLUDED.os_version,
			app_version         = EXCLUDED.app_version,
			last_login_ip       = EXCLUDED.last_login_ip,
			last_login_location = EXCLUDED.last_login_location,
			login_count         = passenger_login_devices.login_count + 1,
			last_login_at       = EXCLUDED.last_login_at,
			updated_at          = EXCLUDED.updated_at`,
		device.PassengerID, device.DeviceID, device.DeviceName, device.DeviceModel,
		device.OSName, device.OSVersion, device.AppVersion, device.LastLoginIP,
		device.LastLoginLocation, device.LoginCount, device.FirstLoginAt, device.LastLoginAt)
	return err
}

const loginDeviceSelect = `device_id, device_name, device_model, os_name, os_version, app_version,
	last_login_ip, last_login_location, login_count, first_login_at, last_login_at`

// List 分页查询，按最近登录时间倒序。
func (r *loginDeviceRepo) List(ctx context.Context, passengerID int64, offset, limit int32) ([]*biz.LoginDevice, int64, error) {
	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM passenger_login_devices WHERE passenger_id = $1`, passengerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*biz.LoginDevice{}, 0, nil
	}

	rows, err := r.data.db.Query(ctx,
		`SELECT `+loginDeviceSelect+`
		 FROM passenger_login_devices
		 WHERE passenger_id = $1
		 ORDER BY last_login_at DESC, id DESC
		 LIMIT $2 OFFSET $3`, passengerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*biz.LoginDevice, 0, limit)
	for rows.Next() {
		device := &biz.LoginDevice{PassengerID: passengerID}
		if err := rows.Scan(&device.DeviceID, &device.DeviceName, &device.DeviceModel,
			&device.OSName, &device.OSVersion, &device.AppVersion,
			&device.LastLoginIP, &device.LastLoginLocation, &device.LoginCount,
			&device.FirstLoginAt, &device.LastLoginAt); err != nil {
			return nil, 0, err
		}
		items = append(items, device)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PruneOldest 只保留最近登录的 keep 台设备（超出部分淘汰最久未登录的）。
// SQL 带 passenger_id 条件并限定删除范围（NOT IN 子查询），不存在全表删除风险。
func (r *loginDeviceRepo) PruneOldest(ctx context.Context, passengerID int64, keep int32) error {
	if keep <= 0 {
		return nil
	}
	_, err := r.data.db.Exec(ctx,
		`DELETE FROM passenger_login_devices
		 WHERE passenger_id = $1
		   AND id NOT IN (
				SELECT id FROM passenger_login_devices
				WHERE passenger_id = $1
				ORDER BY last_login_at DESC, id DESC
				LIMIT $2
		   )`, passengerID, keep)
	return err
}

type loginRecordRepo struct {
	data *Data
	log  *log.Helper
}

// NewLoginRecordRepo 构造函数（wire 注入）。
func NewLoginRecordRepo(d *Data, logger log.Logger) biz.LoginRecordRepo {
	return &loginRecordRepo{
		data: d,
		log:  log.NewHelper(log.With(logger, "module", "data/login_record")),
	}
}

// List 分页查询登录记录，按时间倒序，可按登录结果筛选。
func (r *loginRecordRepo) List(ctx context.Context, passengerID int64, loginResult *int16, offset, limit int32) ([]*biz.LoginRecord, int64, error) {
	args := []any{passengerID}
	where := "passenger_id = $1"
	if loginResult != nil {
		args = append(args, *loginResult)
		where += fmt.Sprintf(" AND login_result = $%d", len(args))
	}

	var total int64
	if err := r.data.db.QueryRow(ctx,
		`SELECT count(*) FROM passenger_auth_records WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*biz.LoginRecord{}, 0, nil
	}

	args = append(args, limit, offset)
	query := `SELECT id, created_at, device_id, device_name, device_model, client_ip, login_location,
			auth_type, login_result, failure_reason
		FROM passenger_auth_records
		WHERE ` + where + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.data.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*biz.LoginRecord, 0, limit)
	for rows.Next() {
		var record biz.LoginRecord
		if err := rows.Scan(&record.ID, &record.LoginAt, &record.DeviceID, &record.DeviceName,
			&record.DeviceModel, &record.LoginIP, &record.LoginLocation,
			&record.AuthType, &record.LoginResult, &record.FailureReason); err != nil {
			return nil, 0, err
		}
		items = append(items, &record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
