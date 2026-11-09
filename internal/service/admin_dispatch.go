package service

import (
	"strconv"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

type adminDispatchItem struct {
	DispatchNo                string   `json:"dispatchNo"`
	OrderID                   int64    `json:"orderId"`
	OrderNo                   string   `json:"orderNo"`
	VehicleID                 *int64   `json:"vehicleId"`
	PlateNo                   string   `json:"plateNo"`
	DispatchStrategy          string   `json:"dispatchStrategy"`
	CandidateScore            *float64 `json:"candidateScore"`
	EstimatedPickupDistanceKM *float64 `json:"estimatedPickupDistanceKm"`
	EstimatedArrivalMin       *int32   `json:"estimatedArrivalMin"`
	Status                    int16    `json:"status"`
	FailureReason             string   `json:"failureReason"`
	DispatchedAt              string   `json:"dispatchedAt"`
	RespondedAt               string   `json:"respondedAt"`
	CreatedAt                 string   `json:"createdAt"`
}

// AdminListDispatchRecords 后台派单记录列表。
func (s *RideService) AdminListDispatchRecords(ctx kratoshttp.Context) error {
	if !s.internalCaller(ctx) {
		return kratoserrors.Forbidden("FORBIDDEN", "internal token required")
	}
	page, _ := strconv.Atoi(ctx.Query().Get("page"))
	pageSize, _ := strconv.Atoi(ctx.Query().Get("pageSize"))
	filter := biz.AdminDispatchRecordFilter{Page: page, PageSize: pageSize, Keyword: ctx.Query().Get("keyword")}
	if value := ctx.Query().Get("status"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 4 {
			return kratoserrors.BadRequest("INVALID_ARGUMENT", "invalid status")
		}
		status := int16(parsed)
		filter.Status = &status
	}
	result, err := s.uc.AdminListDispatchRecords(ctx, filter)
	if err != nil {
		return serviceError(err)
	}
	items := make([]adminDispatchItem, 0, len(result.Items))
	for _, record := range result.Items {
		items = append(items, adminItemFromDispatch(record))
	}
	return ctx.Result(200, map[string]any{"items": items, "total": result.Total, "page": filter.Page, "pageSize": filter.PageSize})
}

func adminItemFromDispatch(record *biz.AdminDispatchRecord) adminDispatchItem {
	return adminDispatchItem{
		DispatchNo: record.DispatchNo, OrderID: record.OrderID, OrderNo: record.OrderNo,
		VehicleID: record.VehicleID, PlateNo: record.PlateNo, DispatchStrategy: record.DispatchStrategy,
		CandidateScore: record.CandidateScore, EstimatedPickupDistanceKM: record.EstimatedPickupDistanceKM,
		EstimatedArrivalMin: record.EstimatedArrivalMin, Status: record.Status, FailureReason: record.FailureReason,
		DispatchedAt: formatOptionalTime(record.DispatchedAt), RespondedAt: formatOptionalTime(record.RespondedAt),
		CreatedAt: record.CreatedAt.Format(time.RFC3339),
	}
}
