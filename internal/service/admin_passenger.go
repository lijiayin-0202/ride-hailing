package service

import (
	"strconv"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type adminPassengerItem struct {
	PassengerID      int64  `json:"passengerId"`
	PassengerNo      string `json:"passengerNo"`
	Mobile           string `json:"mobile"`
	Nickname         string `json:"nickname"`
	AvatarURL        string `json:"avatarUrl"`
	ExternalProvider string `json:"externalProvider"`
	RealNameStatus   int16  `json:"realNameStatus"`
	AccountStatus    int16  `json:"accountStatus"`
	PointBalance     int32  `json:"pointBalance"`
	LastLoginAt      string `json:"lastLoginAt"`
	CreatedAt        string `json:"createdAt"`
}

// AdminListPassengers 后台乘客列表。
func (s *RideService) AdminListPassengers(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	page, _ := strconv.Atoi(ctx.Query().Get("page"))
	pageSize, _ := strconv.Atoi(ctx.Query().Get("pageSize"))
	filter := biz.AdminPassengerFilter{Page: page, PageSize: pageSize, Keyword: ctx.Query().Get("keyword")}
	if value := ctx.Query().Get("realNameStatus"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid realNameStatus")
		}
		status := int16(parsed)
		filter.RealNameStatus = &status
	}
	if value := ctx.Query().Get("accountStatus"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid accountStatus")
		}
		status := int16(parsed)
		filter.AccountStatus = &status
	}
	result, err := s.uc.AdminListPassengers(ctx, filter)
	if err != nil {
		return serviceError(err)
	}
	items := make([]adminPassengerItem, 0, len(result.Items))
	for _, passenger := range result.Items {
		items = append(items, adminItemFromPassenger(passenger))
	}
	return ctx.Result(200, map[string]any{"items": items, "total": result.Total, "page": filter.Page, "pageSize": filter.PageSize})
}

// AdminGetPassenger 后台乘客详情。
func (s *RideService) AdminGetPassenger(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	passengerID, err := parseID(ctx.Vars().Get("passenger_id"))
	if err != nil {
		return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid passengerId")
	}
	passenger, usecaseErr := s.uc.AdminGetPassenger(ctx, passengerID)
	if usecaseErr != nil {
		return serviceError(usecaseErr)
	}
	return ctx.Result(200, map[string]any{"passenger": adminItemFromPassenger(passenger)})
}

func adminItemFromPassenger(passenger *biz.AdminPassenger) adminPassengerItem {
	return adminPassengerItem{
		PassengerID: passenger.ID, PassengerNo: passenger.PassengerNo, Mobile: passenger.Mobile,
		Nickname: passenger.Nickname, AvatarURL: passenger.AvatarURL, ExternalProvider: passenger.ExternalProvider,
		RealNameStatus: passenger.RealNameStatus, AccountStatus: passenger.AccountStatus,
		PointBalance: passenger.PointBalance, LastLoginAt: formatOptionalTime(passenger.LastLoginAt),
		CreatedAt: passenger.CreatedAt.Format(time.RFC3339),
	}
}
