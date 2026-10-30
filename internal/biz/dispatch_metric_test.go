package biz

import (
	"math"
	"testing"
)

func TestMeasureCandidateCalculatesPickupDistanceAndETA(t *testing.T) {
	candidate, err := MeasureCandidate(
		101,
		GeoPoint{Longitude: 116.397, Latitude: 39.908},
		GeoPoint{Longitude: 116.407, Latitude: 39.908},
		30,
	)
	if err != nil {
		t.Fatalf("MeasureCandidate() error = %v", err)
	}
	if candidate.PickupDistanceKM <= 0 || candidate.EstimatedArrivalMin != 2 {
		t.Fatalf("MeasureCandidate() = %#v, want positive distance and two-minute ETA", candidate)
	}
	if math.Abs(candidate.Score-(1/(1+candidate.PickupDistanceKM))) > 0.000001 {
		t.Fatalf("Score = %v, want distance-based score", candidate.Score)
	}
}

func TestEstimateArrivalMinRoundsUp(t *testing.T) {
	if got := EstimateArrivalMin(1.01, 30); got != 3 {
		t.Fatalf("EstimateArrivalMin() = %d, want 3", got)
	}
}

func TestSortCandidatesUsesDistanceThenVehicleID(t *testing.T) {
	candidates := []CandidateVehicle{
		{ID: 22, PickupDistanceKM: 1},
		{ID: 11, PickupDistanceKM: 1},
		{ID: 33, PickupDistanceKM: 0.5},
	}

	SortCandidates(candidates)

	if candidates[0].ID != 33 || candidates[1].ID != 11 || candidates[2].ID != 22 {
		t.Fatalf("SortCandidates() = %#v, want distance then vehicle ID ordering", candidates)
	}
}
