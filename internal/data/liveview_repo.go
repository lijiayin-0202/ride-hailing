package data

import (
	"context"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// GetLiveView 聚合同一订单的订单本体、最新位置、派单、上车核验和安全事件摘要。
// 依赖模块（派单/车端）尚未写入数据时，对应摘要返回 nil，不影响订单主链路。
func (r *rideRepo) GetLiveView(ctx context.Context, passengerID, orderID int64) (*biz.OrderLiveView, error) {
	order, err := r.GetOrder(ctx, passengerID, orderID)
	if err != nil {
		return nil, err
	}
	return &biz.OrderLiveView{
		Order:    order,
		Location: r.latestLocation(ctx, orderID),
		Dispatch: r.dispatchSummary(ctx, orderID),
		Boarding: r.boardingVerification(ctx, orderID),
		Safety:   r.safetyEvents(ctx, orderID),
	}, nil
}

// latestLocation 读取 trip_location_logs 最新一条，轨迹表已带进度与剩余距离，无需重算。
func (r *rideRepo) latestLocation(ctx context.Context, orderID int64) *biz.LocationPoint {
	row := r.firstRow(ctx, `SELECT * FROM trip_location_logs WHERE order_id = ? ORDER BY reported_at DESC LIMIT 1`, orderID)
	if row == nil {
		return nil
	}
	longitude := mapFloat(row, "longitude", "lng")
	latitude := mapFloat(row, "latitude", "lat")
	if longitude == 0 && latitude == 0 {
		return nil
	}
	point := &biz.LocationPoint{
		Longitude:               longitude,
		Latitude:                latitude,
		SpeedKMH:                mapFloat(row, "speed_kmh", "speed"),
		ProgressPercent:         int32(mapFloat(row, "route_progress_percent")),
		RemainingDistanceMeters: int32(mapFloat(row, "remaining_distance_km") * 1000),
	}
	if reportedAt := mapTime(row, "reported_at", "created_at"); reportedAt != nil {
		point.ReportedAt = *reportedAt
	}
	if etaAt := mapTime(row, "eta_at"); etaAt != nil && !point.ReportedAt.IsZero() {
		point.RemainingDurationSeconds = int32(etaAt.Sub(point.ReportedAt).Seconds())
	}
	return point
}

// dispatchSummary 读取 dispatch_records：status 0 已创建 / 1 已下发 / 2 车辆确认 / 3 失败 / 4 超时。
func (r *rideRepo) dispatchSummary(ctx context.Context, orderID int64) *biz.DispatchSummary {
	row := r.firstRow(ctx, `SELECT * FROM dispatch_records WHERE order_id = ? ORDER BY id DESC LIMIT 1`, orderID)
	if row == nil {
		return nil
	}
	status := mapInt64(row, "status")
	summary := &biz.DispatchSummary{
		Strategy:             mapString(row, "dispatch_strategy", "strategy_name"),
		Status:               dispatchStatusText(status),
		VehicleResponse:      mapString(row, "failure_reason"),
		AttemptCount:         1,
		PickupETASeconds:     int32(mapInt64(row, "estimated_arrival_min") * 60),
		PickupDistanceMeters: mapFloat(row, "estimated_pickup_distance_km") * 1000,
		DispatchedAt:         mapTime(row, "dispatched_at", "created_at"),
	}
	if respondedAt := mapTime(row, "responded_at"); respondedAt != nil {
		summary.VehicleResponse = "RESPONDED"
	}
	return summary
}

func dispatchStatusText(status int64) string {
	return map[int64]string{0: "CREATED", 1: "DISPATCHED", 2: "ACCEPTED", 3: "FAILED", 4: "TIMEOUT"}[status]
}

// boardingVerification 读取 boarding_verifications：verification_type 1 手机号后四位 / 2 NFC / 3 动态码；attempt_result 0 失败 / 1 成功。
func (r *rideRepo) boardingVerification(ctx context.Context, orderID int64) *biz.BoardingVerification {
	row := r.firstRow(ctx, `SELECT * FROM boarding_verifications WHERE order_id = ? ORDER BY id DESC LIMIT 1`, orderID)
	if row == nil {
		return nil
	}
	return &biz.BoardingVerification{
		Method:     verificationTypeText(mapInt64(row, "verification_type")),
		Status:     attemptResultText(mapInt64(row, "attempt_result")),
		VerifiedAt: mapTime(row, "verified_at"),
		Attempts:   1,
	}
}

func verificationTypeText(value int64) string {
	return map[int64]string{1: "PHONE_SUFFIX", 2: "NFC", 3: "DYNAMIC_CODE"}[value]
}

func attemptResultText(value int64) string {
	return map[int64]string{0: "FAILED", 1: "VERIFIED"}[value]
}

// safetyEvents 优先读取 safety_incidents（乘客侧安全事件），回退到车辆故障。
func (r *rideRepo) safetyEvents(ctx context.Context, orderID int64) []biz.SafetyEvent {
	rows := r.allRows(ctx, `SELECT * FROM safety_incidents WHERE order_id = ? ORDER BY id DESC LIMIT 20`, orderID)
	events := make([]biz.SafetyEvent, 0, len(rows))
	for _, row := range rows {
		event := biz.SafetyEvent{
			ID:          mapInt64(row, "id"),
			Type:        incidentTypeText(mapInt64(row, "incident_type")),
			Level:       severityText(mapInt64(row, "severity", "fault_level")),
			Description: mapString(row, "description", "fault_description"),
			Status:      incidentStatusText(mapInt64(row, "status")),
		}
		if occurredAt := mapTime(row, "created_at", "occurred_at"); occurredAt != nil {
			event.OccurredAt = *occurredAt
		}
		events = append(events, event)
	}
	if len(events) > 0 {
		return events
	}
	for _, row := range r.allRows(ctx, `SELECT * FROM vehicle_faults WHERE related_order_id = ? ORDER BY id DESC LIMIT 20`, orderID) {
		event := biz.SafetyEvent{
			ID:          mapInt64(row, "id"),
			Type:        mapString(row, "fault_code"),
			Level:       mapString(row, "fault_level"),
			Description: mapString(row, "fault_description"),
			Status:      vehicleFaultStatusText(mapInt64(row, "status")),
		}
		if occurredAt := mapTime(row, "occurred_at", "created_at"); occurredAt != nil {
			event.OccurredAt = *occurredAt
		}
		events = append(events, event)
	}
	return events
}

func incidentTypeText(value int64) string {
	return map[int64]string{1: "VEHICLE_FAULT", 2: "PASSENGER_EMERGENCY", 3: "TRAFFIC_ACCIDENT", 4: "OTHER"}[value]
}

func severityText(value int64) string {
	return map[int64]string{1: "LOW", 2: "MEDIUM", 3: "HIGH", 4: "CRITICAL"}[value]
}

func incidentStatusText(value int64) string {
	return map[int64]string{0: "PENDING", 1: "PROCESSING", 2: "RESOLVED", 3: "CLOSED"}[value]
}

func vehicleFaultStatusText(value int64) string {
	return map[int64]string{0: "PENDING", 1: "PROCESSING", 2: "RECOVERED", 3: "CLOSED"}[value]
}
