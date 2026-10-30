package biz

import (
	"errors"
	"math"
	"sort"
)

const earthRadiusKM = 6371.0088

var ErrInvalidDispatchCoordinate = errors.New("派单坐标无效")
var ErrInvalidDispatchSpeed = errors.New("派单平均速度无效")

// MeasureCandidate 计算车辆到上车点的直线距离、预计到达时间和候选分数。
func MeasureCandidate(vehicleID int64, vehicle, pickup GeoPoint, speedKMH float64) (CandidateVehicle, error) {
	if vehicleID == 0 || !validGeoPoint(vehicle) || !validGeoPoint(pickup) {
		return CandidateVehicle{}, ErrInvalidDispatchCoordinate
	}
	if speedKMH <= 0 {
		return CandidateVehicle{}, ErrInvalidDispatchSpeed
	}
	// 使用坐标直线距离而非道路导航距离：该值用于候选排序与可解释的粗略 ETA，不作为计费依据。
	distanceKM := HaversineDistanceKM(vehicle, pickup)
	return CandidateVehicle{
		ID: vehicleID, Score: candidateScore(distanceKM), PickupDistanceKM: distanceKM,
		EstimatedArrivalMin: EstimateArrivalMin(distanceKM, speedKMH),
	}, nil
}

// HaversineDistanceKM 返回两点之间的球面直线距离，单位为千米。
func HaversineDistanceKM(from, to GeoPoint) float64 {
	latitudeDelta := radians(to.Latitude - from.Latitude)
	longitudeDelta := radians(to.Longitude - from.Longitude)
	a := math.Sin(latitudeDelta/2)*math.Sin(latitudeDelta/2) +
		math.Cos(radians(from.Latitude))*math.Cos(radians(to.Latitude))*
			math.Sin(longitudeDelta/2)*math.Sin(longitudeDelta/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// EstimateArrivalMin 依据直线距离和平均速度向上取整预计到达分钟数。
func EstimateArrivalMin(distanceKM, speedKMH float64) int32 {
	// 向上取整避免向乘客展示“0 分钟到达”或低估接驾时间。
	return int32(math.Ceil(distanceKM / speedKMH * 60))
}

// SortCandidates 按接驾距离升序，车辆 ID 作为稳定兜底顺序。
func SortCandidates(candidates []CandidateVehicle) {
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].PickupDistanceKM == candidates[right].PickupDistanceKM {
			return candidates[left].ID < candidates[right].ID
		}
		return candidates[left].PickupDistanceKM < candidates[right].PickupDistanceKM
	})
}

func candidateScore(distanceKM float64) float64 {
	// 分数只用于记录策略决策；排序唯一依据仍是距离和车辆 ID，保证结果可复现。
	return 1 / (1 + distanceKM)
}

func validGeoPoint(point GeoPoint) bool {
	return point.Longitude >= -180 && point.Longitude <= 180 && point.Latitude >= -90 && point.Latitude <= 90
}

func radians(degrees float64) float64 {
	return degrees * math.Pi / 180
}
