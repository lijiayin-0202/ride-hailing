package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceAreaAvailabilityUsecaseIsOperatingAt(t *testing.T) {
	location := shanghaiLocation(t)
	monday := time.Date(2026, time.November, 2, 0, 0, 0, 0, location)
	tuesday := monday.AddDate(0, 0, 1)
	cases := append(weeklyAvailabilityCases(monday, tuesday), specialDateAvailabilityCases(monday, tuesday)...)

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &fakeServiceAreaRepository{area: testCase.area, schedules: testCase.schedules}
			usecase := NewServiceAreaAvailabilityUsecase(repository)
			got, err := usecase.IsOperatingAt(context.Background(), 1, testCase.instant)
			if err != nil {
				t.Fatalf("IsOperatingAt() error = %v", err)
			}
			if got != testCase.want {
				t.Errorf("IsOperatingAt() = %t, want %t", got, testCase.want)
			}
		})
	}
}

type availabilityCase struct {
	name      string
	area      ServiceArea
	schedules []ServiceAreaSchedule
	instant   time.Time
	want      bool
}

func weeklyAvailabilityCases(monday, tuesday time.Time) []availabilityCase {
	return []availabilityCase{weeklyOpenCase(monday), closedAreaCase(monday), overnightWeeklyCase(tuesday), noScheduleCase(monday)}
}

func weeklyOpenCase(monday time.Time) availabilityCase {
	return availabilityCase{name: "operates during weekly schedule", area: ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 540, EndMinute: 1080, IsEnabled: true}}, instant: at(monday, 10, 0), want: true}
}

func closedAreaCase(monday time.Time) availabilityCase {
	return availabilityCase{name: "rejects non operating area", area: ServiceArea{ID: 1, Status: ServiceAreaStatusTemporarilyClosed}, instant: at(monday, 10, 0)}
}

func overnightWeeklyCase(tuesday time.Time) availabilityCase {
	return availabilityCase{name: "inherits weekly overnight schedule", area: ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 1320, EndMinute: 120, IsEnabled: true}}, instant: at(tuesday, 1, 0), want: true}
}

func noScheduleCase(monday time.Time) availabilityCase {
	return availabilityCase{name: "defaults to unavailable without schedules", area: ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, instant: at(monday, 10, 0)}
}

func specialDateAvailabilityCases(monday, tuesday time.Time) []availabilityCase {
	return []availabilityCase{specialOverrideCase(monday), specialBlocksOvernightCase(tuesday)}
}

func specialOverrideCase(monday time.Time) availabilityCase {
	return availabilityCase{name: "special date overrides weekly schedule", area: ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 540, EndMinute: 1320, IsEnabled: true}, {SpecialDate: monday, StartMinute: 600, EndMinute: 1080, IsEnabled: true}}, instant: at(monday, 9, 30)}
}

func specialBlocksOvernightCase(tuesday time.Time) availabilityCase {
	return availabilityCase{name: "special date blocks previous overnight schedule", area: ServiceArea{ID: 1, Status: ServiceAreaStatusOperating}, schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 1320, EndMinute: 120, IsEnabled: true}, {SpecialDate: tuesday, StartMinute: 600, EndMinute: 1080, IsEnabled: true}}, instant: at(tuesday, 1, 0)}
}

func TestServiceAreaAvailabilityUsecaseRejectsInvalidAreaID(t *testing.T) {
	usecase := NewServiceAreaAvailabilityUsecase(&fakeServiceAreaRepository{})

	_, err := usecase.IsOperatingAt(context.Background(), 0, time.Now())
	if !errors.Is(err, ErrInvalidServiceAreaID) {
		t.Errorf("IsOperatingAt() error = %v, want ErrInvalidServiceAreaID", err)
	}
}

func TestValidateServiceAreaSchedules(t *testing.T) {
	date := time.Date(2026, time.November, 2, 0, 0, 0, 0, shanghaiLocation(t))
	testCases := []struct {
		name      string
		schedules []ServiceAreaSchedule
		wantErr   bool
	}{
		{name: "allows adjacent schedules", schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 540, EndMinute: 720, IsEnabled: true}, {DayOfWeek: 1, StartMinute: 720, EndMinute: 1080, IsEnabled: true}}},
		{name: "rejects overlapping schedules", schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 540, EndMinute: 720, IsEnabled: true}, {DayOfWeek: 1, StartMinute: 600, EndMinute: 1080, IsEnabled: true}}, wantErr: true},
		{name: "rejects invalid weekday", schedules: []ServiceAreaSchedule{{DayOfWeek: 8, StartMinute: 540, EndMinute: 720, IsEnabled: true}}, wantErr: true},
		{name: "rejects equal times", schedules: []ServiceAreaSchedule{{DayOfWeek: 1, StartMinute: 540, EndMinute: 540, IsEnabled: true}}, wantErr: true},
		{name: "rejects special date with weekday", schedules: []ServiceAreaSchedule{{DayOfWeek: 1, SpecialDate: date, StartMinute: 540, EndMinute: 720, IsEnabled: true}}, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateServiceAreaSchedules(testCase.schedules)
			if (err != nil) != testCase.wantErr {
				t.Errorf("ValidateServiceAreaSchedules() error = %v, wantErr %t", err, testCase.wantErr)
			}
		})
	}
}

type fakeServiceAreaRepository struct {
	area      ServiceArea
	schedules []ServiceAreaSchedule
}

func (repository *fakeServiceAreaRepository) Get(context.Context, int64) (ServiceArea, error) {
	return repository.area, nil
}

func (repository *fakeServiceAreaRepository) ListSchedules(context.Context, int64, time.Time, time.Time) ([]ServiceAreaSchedule, error) {
	return repository.schedules, nil
}

func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}

func shanghaiLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	return location
}
