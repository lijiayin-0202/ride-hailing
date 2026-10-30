// 模块1 用户账号与个人中心（成员A）
// 登录设备与登录记录用例（账号管理页「登录设备」「登录记录」）。
// 数据表：passenger_login_devices（设备）、passenger_auth_records（登录记录，写入在 AuthUsecase）
package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	// maxLoginDevices 单个乘客保留的最大设备数，超出后淘汰最久未登录的设备，防止刷表。
	maxLoginDevices = 20
	// maxDeviceIDLen 设备编号长度上限（与列宽 varchar(64) 一致）。
	maxDeviceIDLen = 64
	// unknownDeviceName 设备信息缺失时的兜底名称。
	unknownDeviceName = "未知设备"
)

// LoginDevice 登录设备（passenger_login_devices）。
type LoginDevice struct {
	PassengerID       int64
	DeviceID          string
	DeviceName        string
	DeviceModel       string
	OSName            string
	OSVersion         string
	AppVersion        string
	LastLoginIP       string
	LastLoginLocation string
	LoginCount        int32
	FirstLoginAt      time.Time
	LastLoginAt       time.Time
}

// LoginRecord 登录记录（passenger_auth_records）。
type LoginRecord struct {
	ID            int64
	LoginAt       time.Time
	DeviceID      string
	DeviceName    string
	DeviceModel   string
	LoginIP       string
	LoginLocation string
	AuthType      int16
	LoginResult   int16
	FailureReason string
}

// DeviceLoginInfo 一次登录产生的设备上下文（由接口层从 User-Agent / 请求头解析）。
type DeviceLoginInfo struct {
	// DeviceID 客户端持久化的设备编号，可为空（空时按 UA+IP 生成稳定编号）。
	DeviceID    string
	DeviceName  string
	DeviceModel string
	OSName      string
	OSVersion   string
	AppVersion  string
	// IP 最近登录 IP 原文（仅入库/审计，接口返回前脱敏）。
	IP string
	// Location 归属地展示文案（见 pkg/iputil.ResolveLocation）。
	Location string
}

// LoginDeviceRepo 登录设备数据访问接口。
type LoginDeviceRepo interface {
	// Upsert 按 (passenger_id, device_id) 插入或更新：命中则次数 +1、刷新最近登录信息。
	Upsert(ctx context.Context, device *LoginDevice) error
	List(ctx context.Context, passengerID int64, offset, limit int32) ([]*LoginDevice, int64, error)
	// PruneOldest 只保留最近登录的 keep 台设备，用于控制单乘客设备数量。
	PruneOldest(ctx context.Context, passengerID int64, keep int32) error
}

// LoginRecordRepo 登录记录数据访问接口。
type LoginRecordRepo interface {
	List(ctx context.Context, passengerID int64, loginResult *int16, offset, limit int32) ([]*LoginRecord, int64, error)
}

// LoginDeviceListResult 设备分页结果。
type LoginDeviceListResult struct {
	Items    []*LoginDevice
	Total    int64
	Page     int32
	PageSize int32
}

// LoginRecordListResult 登录记录分页结果。
type LoginRecordListResult struct {
	Items    []*LoginRecord
	Total    int64
	Page     int32
	PageSize int32
}

// LoginDeviceUsecase 登录设备与登录记录用例。
type LoginDeviceUsecase struct {
	deviceRepo LoginDeviceRepo
	recordRepo LoginRecordRepo
	log        *log.Helper
}

// NewLoginDeviceUsecase 构造函数（wire 注入）。
func NewLoginDeviceUsecase(deviceRepo LoginDeviceRepo, recordRepo LoginRecordRepo, logger log.Logger) *LoginDeviceUsecase {
	return &LoginDeviceUsecase{
		deviceRepo: deviceRepo,
		recordRepo: recordRepo,
		log:        log.NewHelper(log.With(logger, "module", "biz/login_device")),
	}
}

// RecordLogin 登录成功后登记设备。
// 失败不阻断登录：设备列表属于辅助信息，主流程（签发令牌）已经完成。
func (uc *LoginDeviceUsecase) RecordLogin(ctx context.Context, passengerID int64, info *DeviceLoginInfo) error {
	if passengerID <= 0 {
		return ErrUnauthorized
	}
	if info == nil {
		info = &DeviceLoginInfo{}
	}

	now := time.Now()
	device := &LoginDevice{
		PassengerID:       passengerID,
		DeviceID:          normalizeDeviceID(info),
		DeviceName:        firstNonEmptyString(info.DeviceName, unknownDeviceName),
		DeviceModel:       firstNonEmptyString(info.DeviceModel, unknownDeviceName),
		OSName:            info.OSName,
		OSVersion:         info.OSVersion,
		AppVersion:        info.AppVersion,
		LastLoginIP:       info.IP,
		LastLoginLocation: firstNonEmptyString(info.Location, "未知归属地"),
		LoginCount:        1,
		FirstLoginAt:      now,
		LastLoginAt:       now,
	}
	if err := uc.deviceRepo.Upsert(ctx, device); err != nil {
		uc.log.WithContext(ctx).Warnf("登记登录设备失败 passenger_id=%d device_id=%s err=%v", passengerID, device.DeviceID, err)
		return err
	}
	// 设备数超限时淘汰最久未登录的设备；失败只记录，不影响登录。
	if err := uc.deviceRepo.PruneOldest(ctx, passengerID, maxLoginDevices); err != nil {
		uc.log.WithContext(ctx).Warnf("清理历史登录设备失败 passenger_id=%d err=%v", passengerID, err)
	}
	return nil
}

// ListDevices 分页查询登录设备，按最近登录时间倒序（实现见 data 层）。
func (uc *LoginDeviceUsecase) ListDevices(ctx context.Context, passengerID int64, page, pageSize int32) (*LoginDeviceListResult, error) {
	page, pageSize, offset := NormalizePage(page, pageSize)
	items, total, err := uc.deviceRepo.List(ctx, passengerID, offset, pageSize)
	if err != nil {
		return nil, err
	}
	return &LoginDeviceListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListRecords 分页查询登录记录，可按登录结果筛选。
func (uc *LoginDeviceUsecase) ListRecords(ctx context.Context, passengerID int64, page, pageSize int32, loginResult *int32) (*LoginRecordListResult, error) {
	if loginResult != nil && int32(*loginResult) != int32(LoginResultFailed) && int32(*loginResult) != int32(LoginResultSuccess) {
		return nil, invalidArgument("登录结果筛选值不合法")
	}
	page, pageSize, offset := NormalizePage(page, pageSize)

	var filter *int16
	if loginResult != nil {
		value := int16(*loginResult)
		filter = &value
	}
	items, total, err := uc.recordRepo.List(ctx, passengerID, filter, offset, pageSize)
	if err != nil {
		return nil, err
	}
	return &LoginRecordListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// normalizeDeviceID 规范化设备编号：空值按 UA+IP 生成稳定编号，超长截断。
// 说明：H5 端 localStorage 会持久化 device_id；未上报时用 UA+IP 兜底，
// 保证同一设备重复登录归并为一条，而不是每次新增。
func normalizeDeviceID(info *DeviceLoginInfo) string {
	id := strings.TrimSpace(info.DeviceID)
	if id == "" {
		sum := sha256.Sum256([]byte(info.DeviceModel + "|" + info.OSName + "|" + info.IP))
		id = "auto-" + hex.EncodeToString(sum[:])[:16]
	}
	if len(id) > maxDeviceIDLen {
		id = id[:maxDeviceIDLen]
	}
	return id
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
