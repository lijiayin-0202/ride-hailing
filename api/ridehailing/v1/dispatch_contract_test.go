package v1

import "testing"

func TestGetDispatchableOrderReplyCarriesDispatchFacts(t *testing.T) {
	reply := &GetDispatchableOrderReply{
		OrderId:       81,
		ServiceAreaId: 9,
		Pickup: &GeoPoint{
			Longitude: 116.397,
			Latitude:  39.908,
		},
		IsSchedulable: true,
	}

	if reply.GetOrderId() != 81 || reply.GetServiceAreaId() != 9 || !reply.GetIsSchedulable() {
		t.Fatalf("GetDispatchableOrderReply() = %#v, want dispatchable order facts", reply)
	}
}
