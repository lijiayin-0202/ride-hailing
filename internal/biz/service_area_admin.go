package biz

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidManagedServiceArea = errors.New("运营区域资料无效")
	ErrImmutableServiceAreaCode  = errors.New("运营区域编码不可修改")
	ErrServiceAreaNotFound       = errors.New("运营区域不存在")
	ErrServiceAreaCodeConflict   = errors.New("运营区域编码已存在")
	serviceAreaCodePattern       = regexp.MustCompile("^[A-Za-z0-9_-]{1,40}$")
)

// ManagedServiceArea 是后台维护运营区域时所需的完整资料。
type ManagedServiceArea struct {
	ID              int64
	AreaCode        string
	AreaName        string
	CityCode        string
	CityName        string
	BoundaryGeoJSON string
	ServiceNotice   string
	Status          int16
}

// ManagedServiceAreaSchedule 保留后台维护的时段公告，其余规则交由既有时段校验复用。
type ManagedServiceAreaSchedule struct {
	DayOfWeek   int
	SpecialDate string
	StartMinute int
	EndMinute   int
	IsEnabled   bool
	Notice      string
}

// ManagedServiceAreaDetail 同时返回区域资料和完整时段规则。
type ManagedServiceAreaDetail struct {
	ServiceArea ManagedServiceArea
	Schedules   []ManagedServiceAreaSchedule
}

// CreateServiceAreaCommand 是创建区域的全部可写资料。
type CreateServiceAreaCommand struct {
	AreaCode        string
	AreaName        string
	CityCode        string
	CityName        string
	BoundaryGeoJSON string
	ServiceNotice   string
	Status          int16
}

// UpdateServiceAreaCommand 的 AreaCode 仅供内部防御性校验，外部协议不提供该字段。
type UpdateServiceAreaCommand struct {
	ID              int64
	AreaCode        string
	AreaName        string
	CityCode        string
	CityName        string
	BoundaryGeoJSON string
	ServiceNotice   string
}

// ServiceAreaListQuery 定义稳定分页与可选筛选条件。
type ServiceAreaListQuery struct {
	Page     int
	PageSize int
	CityCode string
	Status   *int16
}

// ServiceAreaAdminRepository 是运营区域后台管理的持久化端口。
type ServiceAreaAdminRepository interface {
	Create(context.Context, ManagedServiceArea) (ManagedServiceArea, error)
	List(context.Context, ServiceAreaListQuery) ([]ManagedServiceArea, int64, error)
	GetDetail(context.Context, int64) (ManagedServiceAreaDetail, error)
	Update(context.Context, ManagedServiceArea) (ManagedServiceArea, error)
	ChangeStatus(context.Context, int64, int16) (ManagedServiceArea, error)
	ReplaceSchedules(context.Context, int64, []ManagedServiceAreaSchedule) error
}

// ServiceAreaAdminUsecase 负责后台运营区域资料与时段写入前的领域约束。
type ServiceAreaAdminUsecase struct{ repository ServiceAreaAdminRepository }

// NewServiceAreaAdminUsecase 创建运营区域后台管理用例。
func NewServiceAreaAdminUsecase(repository ServiceAreaAdminRepository) *ServiceAreaAdminUsecase {
	return &ServiceAreaAdminUsecase{repository: repository}
}

// Create 校验区域资料后创建新区域。
func (usecase *ServiceAreaAdminUsecase) Create(ctx context.Context, command CreateServiceAreaCommand) (ManagedServiceArea, error) {
	area := ManagedServiceArea{AreaCode: command.AreaCode, AreaName: command.AreaName, CityCode: command.CityCode,
		CityName: command.CityName, BoundaryGeoJSON: command.BoundaryGeoJSON, ServiceNotice: command.ServiceNotice, Status: command.Status}
	if !isValidManagedServiceArea(area) {
		return ManagedServiceArea{}, ErrInvalidManagedServiceArea
	}
	return usecase.repository.Create(ctx, area)
}

// List 校验分页与筛选条件后返回稳定分页结果。
func (usecase *ServiceAreaAdminUsecase) List(ctx context.Context, query ServiceAreaListQuery) ([]ManagedServiceArea, int64, error) {
	if query.Page <= 0 || query.PageSize <= 0 || query.PageSize > 100 ||
		utf8.RuneCountInString(query.CityCode) > 20 || (query.Status != nil && !isValidServiceAreaStatus(*query.Status)) {
		return nil, 0, ErrInvalidManagedServiceArea
	}
	return usecase.repository.List(ctx, query)
}

// GetDetail 拒绝无效 ID，避免无意义的持久化访问。
func (usecase *ServiceAreaAdminUsecase) GetDetail(ctx context.Context, id int64) (ManagedServiceAreaDetail, error) {
	if id <= 0 {
		return ManagedServiceAreaDetail{}, ErrInvalidManagedServiceArea
	}
	return usecase.repository.GetDetail(ctx, id)
}

// Update 在读取当前区域后拒绝任何编码变更，再提交完整可编辑资料。
func (usecase *ServiceAreaAdminUsecase) Update(ctx context.Context, command UpdateServiceAreaCommand) (ManagedServiceArea, error) {
	if command.ID <= 0 {
		return ManagedServiceArea{}, ErrInvalidManagedServiceArea
	}
	detail, err := usecase.repository.GetDetail(ctx, command.ID)
	if err != nil {
		return ManagedServiceArea{}, err
	}
	if command.AreaCode != "" && command.AreaCode != detail.ServiceArea.AreaCode {
		return ManagedServiceArea{}, ErrImmutableServiceAreaCode
	}
	area := ManagedServiceArea{ID: command.ID, AreaCode: detail.ServiceArea.AreaCode, AreaName: command.AreaName,
		CityCode: command.CityCode, CityName: command.CityName, BoundaryGeoJSON: command.BoundaryGeoJSON,
		ServiceNotice: command.ServiceNotice, Status: detail.ServiceArea.Status}
	if !isValidManagedServiceArea(area) {
		return ManagedServiceArea{}, ErrInvalidManagedServiceArea
	}
	return usecase.repository.Update(ctx, area)
}

// ChangeStatus 只接受迁移已定义的三个状态值。
func (usecase *ServiceAreaAdminUsecase) ChangeStatus(ctx context.Context, id int64, status int16) (ManagedServiceArea, error) {
	if id <= 0 || !isValidServiceAreaStatus(status) {
		return ManagedServiceArea{}, ErrInvalidManagedServiceArea
	}
	return usecase.repository.ChangeStatus(ctx, id, status)
}

// ReplaceSchedules 先复用时段形态与重叠校验，空规则列表代表默认不可运营。
func (usecase *ServiceAreaAdminUsecase) ReplaceSchedules(ctx context.Context, id int64, schedules []ManagedServiceAreaSchedule) error {
	if id <= 0 || !isValidManagedSchedules(schedules) {
		return ErrInvalidAreaSchedule
	}
	return usecase.repository.ReplaceSchedules(ctx, id, schedules)
}

func isValidManagedServiceArea(area ManagedServiceArea) bool {
	return serviceAreaCodePattern.MatchString(area.AreaCode) && validRequiredText(area.AreaName, 100) &&
		validRequiredText(area.CityCode, 20) && validRequiredText(area.CityName, 60) && json.Valid([]byte(area.BoundaryGeoJSON)) &&
		validOptionalText(area.ServiceNotice, 500) && isValidServiceAreaStatus(area.Status)
}

func isValidServiceAreaStatus(status int16) bool {
	return status == ServiceAreaStatusTemporarilyClosed || status == ServiceAreaStatusOperating || status == ServiceAreaStatusOffline
}

func isValidManagedSchedules(schedules []ManagedServiceAreaSchedule) bool {
	availabilitySchedules := make([]ServiceAreaSchedule, 0, len(schedules))
	for _, schedule := range schedules {
		parsedDate, validDate := parseScheduleDate(schedule.SpecialDate)
		if !validDate || utf8.RuneCountInString(schedule.Notice) > 300 {
			return false
		}
		availabilitySchedules = append(availabilitySchedules, ServiceAreaSchedule{DayOfWeek: schedule.DayOfWeek,
			SpecialDate: parsedDate, StartMinute: schedule.StartMinute, EndMinute: schedule.EndMinute, IsEnabled: schedule.IsEnabled})
	}
	return ValidateServiceAreaSchedules(availabilitySchedules) == nil
}

func parseScheduleDate(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, true
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	return parsed, err == nil && parsed.Format("2006-01-02") == value
}
