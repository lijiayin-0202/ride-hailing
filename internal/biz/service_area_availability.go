package biz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	// 区域状态是最高优先级开关：临时关闭和停运时，任何时段规则都不能恢复运营。
	ServiceAreaStatusTemporarilyClosed int16 = 0
	ServiceAreaStatusOperating         int16 = 1
	ServiceAreaStatusOffline           int16 = 2
	minutesPerDay                            = 24 * 60
)

var (
	ErrInvalidServiceAreaID = errors.New("运营区域 ID 无效")
	ErrInvalidAreaSchedule  = errors.New("运营时段规则无效")
)

// ServiceArea 是运营区域的最小领域事实。
type ServiceArea struct {
	ID     int64
	Status int16
}

// ServiceAreaSchedule 使用本地分钟表示服务时段。DayOfWeek 与 SpecialDate 二选一。
type ServiceAreaSchedule struct {
	DayOfWeek   int
	SpecialDate time.Time
	StartMinute int
	EndMinute   int
	IsEnabled   bool
}

// ServiceAreaRepository 提供区域与指定日期范围内的服务时段。
type ServiceAreaRepository interface {
	Get(context.Context, int64) (ServiceArea, error)
	ListSchedules(context.Context, int64, time.Time, time.Time) ([]ServiceAreaSchedule, error)
}

// ServiceAreaAvailabilityUsecase 判定运营区域在指定时刻能否运营。
type ServiceAreaAvailabilityUsecase struct {
	repository ServiceAreaRepository
}

// NewServiceAreaAvailabilityUsecase 创建运营区域可用性用例。
func NewServiceAreaAvailabilityUsecase(repository ServiceAreaRepository) *ServiceAreaAvailabilityUsecase {
	return &ServiceAreaAvailabilityUsecase{repository: repository}
}

// IsOperatingAt 按特殊日期、常规时段和前日跨午夜时段的优先级进行判定。
func (usecase *ServiceAreaAvailabilityUsecase) IsOperatingAt(ctx context.Context, serviceAreaID int64, instant time.Time) (bool, error) {
	if serviceAreaID <= 0 {
		return false, ErrInvalidServiceAreaID
	}
	area, err := usecase.repository.Get(ctx, serviceAreaID)
	if err != nil {
		return false, err
	}
	// 先执行区域总开关，再读取时段规则，对应“临时关闭/停运优先”的业务约定。
	if area.Status != ServiceAreaStatusOperating {
		return false, nil
	}
	local, err := instantInShanghai(instant)
	if err != nil {
		return false, err
	}
	day := dateAtShanghaiMidnight(local)
	// 跨午夜时段可能在前一天开始，因此一次读取昨天和今天的规则。
	schedules, err := usecase.repository.ListSchedules(ctx, serviceAreaID, day.AddDate(0, 0, -1), day)
	if err != nil {
		return false, err
	}
	return isOperatingOnLocalTime(schedules, day, local), nil
}

func instantInShanghai(instant time.Time) (time.Time, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, fmt.Errorf("load Asia/Shanghai: %w", err)
	}
	return instant.In(location), nil
}

func isOperatingOnLocalTime(schedules []ServiceAreaSchedule, day, local time.Time) bool {
	// 特殊日期一旦存在即完全覆盖同日星期常规规则；无匹配即默认不可运营。
	if hasSpecialScheduleOn(schedules, day) {
		return matchesSchedule(specialSchedulesOn(schedules, day), minuteOfDay(local), false)
	}
	if matchesSchedule(weeklySchedulesOn(schedules, day), minuteOfDay(local), false) {
		return true
	}
	return matchesSchedule(weeklySchedulesOn(schedules, day.AddDate(0, 0, -1)), minuteOfDay(local), true)
}

func hasSpecialScheduleOn(schedules []ServiceAreaSchedule, day time.Time) bool {
	for _, schedule := range schedules {
		if sameDate(schedule.SpecialDate, day) {
			return true
		}
	}
	return false
}

func specialSchedulesOn(schedules []ServiceAreaSchedule, day time.Time) []ServiceAreaSchedule {
	return filterSchedules(schedules, func(schedule ServiceAreaSchedule) bool {
		return sameDate(schedule.SpecialDate, day)
	})
}

func weeklySchedulesOn(schedules []ServiceAreaSchedule, day time.Time) []ServiceAreaSchedule {
	return filterSchedules(schedules, func(schedule ServiceAreaSchedule) bool {
		return schedule.SpecialDate.IsZero() && schedule.DayOfWeek == int(day.Weekday()+6)%7+1
	})
}

func filterSchedules(schedules []ServiceAreaSchedule, keep func(ServiceAreaSchedule) bool) []ServiceAreaSchedule {
	filtered := make([]ServiceAreaSchedule, 0)
	for _, schedule := range schedules {
		if keep(schedule) {
			filtered = append(filtered, schedule)
		}
	}
	return filtered
}

func matchesSchedule(schedules []ServiceAreaSchedule, minute int, previousDay bool) bool {
	for _, schedule := range schedules {
		if !schedule.IsEnabled || !isScheduleTimeValid(schedule) {
			continue
		}
		if schedule.StartMinute < schedule.EndMinute && !previousDay && minute >= schedule.StartMinute && minute < schedule.EndMinute {
			return true
		}
		if schedule.StartMinute > schedule.EndMinute {
			if !previousDay && minute >= schedule.StartMinute {
				return true
			}
			if previousDay && minute < schedule.EndMinute {
				return true
			}
		}
	}
	return false
}

// ValidateServiceAreaSchedules 校验规则形态及同一规则范围内的时段重叠。
func ValidateServiceAreaSchedules(schedules []ServiceAreaSchedule) error {
	// 将跨午夜区间拆分为两段后再检查重叠，避免 22:00-02:00 漏判。
	groups := make(map[string][]timeInterval)
	for _, schedule := range schedules {
		key, err := scheduleGroupKey(schedule)
		if err != nil {
			return err
		}
		if !schedule.IsEnabled {
			continue
		}
		for _, interval := range scheduleIntervals(schedule) {
			groups[key] = append(groups[key], interval)
		}
	}
	for _, intervals := range groups {
		if hasOverlap(intervals) {
			return fmt.Errorf("%w: 同一规则范围内时段重叠", ErrInvalidAreaSchedule)
		}
	}
	return nil
}

type timeInterval struct{ start, end int }

func scheduleGroupKey(schedule ServiceAreaSchedule) (string, error) {
	if !isScheduleTimeValid(schedule) {
		return "", fmt.Errorf("%w: 起止时间必须在一天内且不能相同", ErrInvalidAreaSchedule)
	}
	if schedule.SpecialDate.IsZero() {
		if schedule.DayOfWeek < 1 || schedule.DayOfWeek > 7 {
			return "", fmt.Errorf("%w: 星期必须介于 1 和 7", ErrInvalidAreaSchedule)
		}
		return fmt.Sprintf("weekday:%d", schedule.DayOfWeek), nil
	}
	if schedule.DayOfWeek != 0 {
		return "", fmt.Errorf("%w: 特殊日期规则不能包含星期", ErrInvalidAreaSchedule)
	}
	return "date:" + dateAtShanghaiMidnight(schedule.SpecialDate).Format("2006-01-02"), nil
}

func isScheduleTimeValid(schedule ServiceAreaSchedule) bool {
	return schedule.StartMinute >= 0 && schedule.StartMinute < minutesPerDay &&
		schedule.EndMinute >= 0 && schedule.EndMinute < minutesPerDay &&
		schedule.StartMinute != schedule.EndMinute
}

func scheduleIntervals(schedule ServiceAreaSchedule) []timeInterval {
	if schedule.StartMinute < schedule.EndMinute {
		return []timeInterval{{start: schedule.StartMinute, end: schedule.EndMinute}}
	}
	return []timeInterval{{start: 0, end: schedule.EndMinute}, {start: schedule.StartMinute, end: minutesPerDay}}
}

func hasOverlap(intervals []timeInterval) bool {
	sort.Slice(intervals, func(left, right int) bool { return intervals[left].start < intervals[right].start })
	for index := 1; index < len(intervals); index++ {
		if intervals[index].start < intervals[index-1].end {
			return true
		}
	}
	return false
}

func dateAtShanghaiMidnight(value time.Time) time.Time {
	location, _ := time.LoadLocation("Asia/Shanghai")
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func sameDate(left, right time.Time) bool {
	return !left.IsZero() && dateAtShanghaiMidnight(left).Equal(dateAtShanghaiMidnight(right))
}

func minuteOfDay(value time.Time) int { return value.Hour()*60 + value.Minute() }
