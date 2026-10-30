package service

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

func TestStationServiceCreateMapsRequest(t *testing.T) {
	usecase := &fakeStationUsecase{station: serviceTestStation()}
	service := NewStationService(usecase)
	request := &v1.CreateStationRequest{ServiceAreaId: 10, StationCode: "BJ-YZ-001",
		StationName: "亦庄上车点", AliasName: "地铁口", Address: "北京示例路 1 号",
		Longitude: 116.4, Latitude: 39.9, StationType: 2, ServiceDirection: 3,
		DirectionHint: "东侧上车"}

	reply, err := service.CreateStation(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateStation() error = %v", err)
	}
	if usecase.create.StationCode != request.GetStationCode() ||
		usecase.create.ServiceAreaID != request.GetServiceAreaId() {
		t.Errorf("CreateStation() command = %#v, want mapped request", usecase.create)
	}
	if reply.GetStation().GetId() != 1001 || reply.GetStation().GetStatus() != 1 {
		t.Errorf("CreateStation() reply = %#v, want mapped station", reply)
	}
}

func TestStationServiceUpdateMapsOnlyEditableFields(t *testing.T) {
	usecase := &fakeStationUsecase{station: serviceTestStation()}
	service := NewStationService(usecase)
	request := &v1.UpdateStationRequest{Id: 1001, StationName: "新名称", Address: "新地址",
		Longitude: 116.5, Latitude: 39.8, StationType: 1, ServiceDirection: 2}

	if _, err := service.UpdateStation(context.Background(), request); err != nil {
		t.Fatalf("UpdateStation() error = %v", err)
	}
	if usecase.update.StationCode != "" || usecase.update.ServiceAreaID != 0 {
		t.Errorf("UpdateStation() immutable fields = code:%q area:%d, want zero values", usecase.update.StationCode, usecase.update.ServiceAreaID)
	}
}

func TestStationServiceChangeStatusMapsRequest(t *testing.T) {
	usecase := &fakeStationUsecase{station: serviceTestStation()}
	service := NewStationService(usecase)

	if _, err := service.ChangeStationStatus(context.Background(), &v1.ChangeStationStatusRequest{Id: 1001, Status: 0}); err != nil {
		t.Fatalf("ChangeStationStatus() error = %v", err)
	}
	if usecase.statusID != 1001 || usecase.status != 0 {
		t.Errorf("ChangeStationStatus() = id:%d status:%d, want id:1001 status:0", usecase.statusID, usecase.status)
	}
}

func TestStationServiceRejectsNilRequest(t *testing.T) {
	service := NewStationService(&fakeStationUsecase{})

	if _, err := service.CreateStation(context.Background(), nil); err == nil {
		t.Fatal("CreateStation(nil) error = nil, want validation error")
	}
}

func TestStationServiceMapsInvalidStationToBadRequest(t *testing.T) {
	service := NewStationService(&fakeStationUsecase{err: biz.ErrInvalidStation})

	_, err := service.CreateStation(context.Background(), &v1.CreateStationRequest{})
	if err == nil {
		t.Fatal("CreateStation() error = nil, want bad request")
	}
	if got := kratoserrors.Code(err); got != 400 {
		t.Fatalf("CreateStation() error code = %d, want 400", got)
	}
	if got := kratoserrors.Reason(err); got != "INVALID_STATION_REQUEST" {
		t.Fatalf("CreateStation() error reason = %q, want INVALID_STATION_REQUEST", got)
	}
}

type fakeStationUsecase struct {
	create   biz.CreateStationCommand
	update   biz.UpdateStationCommand
	statusID int64
	status   int16
	station  biz.Station
	err      error
}

func (usecase *fakeStationUsecase) Create(_ context.Context, command biz.CreateStationCommand) (biz.Station, error) {
	usecase.create = command
	return usecase.station, usecase.err
}

func (usecase *fakeStationUsecase) Update(_ context.Context, command biz.UpdateStationCommand) (biz.Station, error) {
	usecase.update = command
	return usecase.station, usecase.err
}

func (usecase *fakeStationUsecase) ChangeStatus(_ context.Context, id int64, status int16) (biz.Station, error) {
	usecase.statusID, usecase.status = id, status
	return usecase.station, usecase.err
}

func (usecase *fakeStationUsecase) List(context.Context, int64) ([]biz.Station, error) {
	return []biz.Station{usecase.station}, usecase.err
}

func serviceTestStation() biz.Station {
	return biz.Station{ID: 1001, ServiceAreaID: 10, StationCode: "BJ-YZ-001",
		StationName: "亦庄上车点", Address: "北京示例路 1 号", Longitude: 116.4,
		Latitude: 39.9, StationType: 1, ServiceDirection: 3, Status: 1}
}
