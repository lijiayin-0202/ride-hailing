// 模块1 用户账号与个人中心（成员A）
// 常用地址（乘客端「家/公司/常用站点」）用例。
// 数据表：passenger_addresses；station_id 关联 stations.id（stations 由模块3交付）。
package biz

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/log"
)

// 地址类型（passenger_addresses.address_type）
const (
	AddressTypeHome    int32 = 1 // 家
	AddressTypeWork    int32 = 2 // 公司
	AddressTypeStation int32 = 3 // 常用站点
)

// Address 常用地址实体（passenger_addresses）。
type Address struct {
	// ID 地址主键。
	ID int64
	// PassengerID 归属乘客，所有读写都必须带上做数据隔离。
	PassengerID int64
	// AddressType 类型：1 家 / 2 公司 / 3 常用站点。
	AddressType int32
	// AddressName 地址名称（≤60 字符）。
	AddressName string
	// Address 详细地址（≤255 字符）。
	Address string
	// Longitude 经度（GCJ-02，-180~180）。
	Longitude float64
	// Latitude 纬度（GCJ-02，-90~90）。
	Latitude float64
	// StationID 关联 stations.id；模块3站点表未落地前只存编号，不做跨模块联表。
	StationID int64
	// IsDefault 是否默认地址；同一乘客同类型只允许一个。
	IsDefault bool
	// LastUsedAt 最近使用时间；nil 表示从未使用。
	LastUsedAt *time.Time
	// CreatedAt 创建时间。
	CreatedAt time.Time
	// UpdatedAt 更新时间。
	UpdatedAt time.Time
}

// AddressInput 新增/修改入参；指针字段为空表示不修改（PATCH 语义）。
type AddressInput struct {
	// AddressType 地址类型；新增时必填。
	AddressType *int32
	// AddressName 地址名称。
	AddressName *string
	// Address 详细地址。
	Address *string
	// Longitude 经度。
	Longitude *float64
	// Latitude 纬度。
	Latitude *float64
	// StationID 关联站点编号，0 表示不关联。
	StationID *int64
	// IsDefault 是否置为默认。
	IsDefault *bool
}

// AddressListResult 分页结果。
type AddressListResult struct {
	// Items 当前页地址列表。
	Items []*Address
	// Total 满足条件的总数。
	Total int64
	// Page 归一化后的页码。
	Page int32
	// PageSize 归一化后的每页条数。
	PageSize int32
}

// AddressRepo 常用地址数据访问接口。
type AddressRepo interface {
	// List 分页查询本人地址，默认地址优先。
	List(ctx context.Context, passengerID int64, addressType *int32, offset, limit int32) ([]*Address, int64, error)
	// Get 按 id + passengerID 查询；越权访问按不存在处理。
	Get(ctx context.Context, id, passengerID int64) (*Address, error)
	// Create 新增地址；is_default=true 时在同一事务内清除同类型旧默认项。
	Create(ctx context.Context, a *Address) error
	// Update 修改地址；is_default=true 时在同一事务内清除同类型旧默认项。
	Update(ctx context.Context, a *Address) error
	// Delete 删除地址，返回受影响行数；必须同时带 passenger_id 条件。
	Delete(ctx context.Context, id, passengerID int64) (int64, error)
}

// AddressUsecase 常用地址用例。
type AddressUsecase struct {
	repo AddressRepo
	log  *log.Helper
}

// NewAddressUsecase 构造函数（wire 注入）。
func NewAddressUsecase(repo AddressRepo, logger log.Logger) *AddressUsecase {
	return &AddressUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "biz/address")),
	}
}

// List 分页查询当前乘客的常用地址（强制分页）。
func (uc *AddressUsecase) List(ctx context.Context, passengerID int64, page, pageSize int32, addressType *int32) (*AddressListResult, error) {
	if addressType != nil && !validAddressType(*addressType) {
		return nil, invalidArgument("地址类型不合法")
	}
	page, pageSize, offset := NormalizePage(page, pageSize)
	items, total, err := uc.repo.List(ctx, passengerID, addressType, offset, pageSize)
	if err != nil {
		return nil, err
	}
	return &AddressListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Create 新增常用地址。
func (uc *AddressUsecase) Create(ctx context.Context, passengerID int64, in *AddressInput) (*Address, error) {
	if in == nil || in.AddressType == nil {
		return nil, invalidArgument("地址类型不能为空")
	}
	address := &Address{
		PassengerID: passengerID,
		AddressType: *in.AddressType,
	}
	applyAddressPatch(address, in)
	if err := validateAddress(address); err != nil {
		return nil, err
	}
	now := time.Now()
	address.CreatedAt = now
	address.UpdatedAt = now
	if err := uc.repo.Create(ctx, address); err != nil {
		return nil, err
	}
	return address, nil
}

// Update 修改常用地址；先按 passenger_id 校验归属，防止越权改他人地址。
func (uc *AddressUsecase) Update(ctx context.Context, passengerID, id int64, in *AddressInput) (*Address, error) {
	if id <= 0 {
		return nil, invalidArgument("地址ID不合法")
	}
	address, err := uc.repo.Get(ctx, id, passengerID)
	if err != nil {
		return nil, err
	}
	applyAddressPatch(address, in)
	if err := validateAddress(address); err != nil {
		return nil, err
	}
	address.UpdatedAt = time.Now()
	if err := uc.repo.Update(ctx, address); err != nil {
		return nil, err
	}
	return address, nil
}

// Delete 删除常用地址。
// 越权删除与他人地址同样按「资源不存在」返回，避免探测资源是否存在；
// 重复删除返回资源不存在，保证结果可预期。
func (uc *AddressUsecase) Delete(ctx context.Context, passengerID, id int64) error {
	if id <= 0 {
		return invalidArgument("地址ID不合法")
	}
	affected, err := uc.repo.Delete(ctx, id, passengerID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAddressNotFound
	}
	uc.log.WithContext(ctx).Infof("乘客删除常用地址 passenger_id=%d address_id=%d", passengerID, id)
	return nil
}

// applyAddressPatch 把非空字段合并进实体（PATCH 语义）。
func applyAddressPatch(address *Address, in *AddressInput) {
	if in == nil {
		return
	}
	if in.AddressType != nil {
		address.AddressType = *in.AddressType
	}
	if in.AddressName != nil {
		address.AddressName = *in.AddressName
	}
	if in.Address != nil {
		address.Address = *in.Address
	}
	if in.Longitude != nil {
		address.Longitude = *in.Longitude
	}
	if in.Latitude != nil {
		address.Latitude = *in.Latitude
	}
	if in.StationID != nil {
		address.StationID = *in.StationID
	}
	if in.IsDefault != nil {
		address.IsDefault = *in.IsDefault
	}
}

// validateAddress 字段校验：类型枚举、文本长度、坐标范围（GCJ-02）。
func validateAddress(address *Address) error {
	if !validAddressType(address.AddressType) {
		return invalidArgument("地址类型不合法")
	}
	name := trimSpace(address.AddressName)
	if name == "" {
		return invalidArgument("地址名称不能为空")
	}
	if utf8.RuneCountInString(name) > 60 {
		return invalidArgument("地址名称长度不能超过60个字符")
	}
	address.AddressName = name

	detail := trimSpace(address.Address)
	if detail == "" {
		return invalidArgument("详细地址不能为空")
	}
	if utf8.RuneCountInString(detail) > 255 {
		return invalidArgument("详细地址长度不能超过255个字符")
	}
	address.Address = detail

	if address.Longitude < -180 || address.Longitude > 180 {
		return invalidArgument("经度不合法")
	}
	if address.Latitude < -90 || address.Latitude > 90 {
		return invalidArgument("纬度不合法")
	}
	if address.StationID < 0 {
		return invalidArgument("站点编号不合法")
	}
	return nil
}

func validAddressType(t int32) bool {
	return t == AddressTypeHome || t == AddressTypeWork || t == AddressTypeStation
}
