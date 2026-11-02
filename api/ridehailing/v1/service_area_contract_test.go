package v1

import (
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestServiceAreaAdminContractExposesManagementOperations(t *testing.T) {
	service := File_api_ridehailing_v1_service_area_proto.Services().ByName("ServiceAreaAdminService")
	if service == nil {
		t.Fatal("ServiceAreaAdminService is missing")
	}
	wantMethods := []protoreflect.Name{
		"CreateServiceArea",
		"ListServiceAreas",
		"GetServiceArea",
		"UpdateServiceArea",
		"ChangeServiceAreaStatus",
		"ReplaceServiceAreaSchedules",
	}
	if service.Methods().Len() != len(wantMethods) {
		t.Fatalf("ServiceAreaAdminService method count = %d, want %d", service.Methods().Len(), len(wantMethods))
	}
	for _, name := range wantMethods {
		method := service.Methods().ByName(name)
		if method == nil {
			t.Fatalf("ServiceAreaAdminService method %q is missing", name)
		}
		if !method.Options().(interface{ ProtoReflect() protoreflect.Message }).ProtoReflect().Has(annotations.E_Http.TypeDescriptor()) {
			t.Fatalf("ServiceAreaAdminService method %q has no HTTP annotation", name)
		}
	}
	if ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED != 0 ||
		ServiceAreaStatus_SERVICE_AREA_STATUS_OPERATING != 1 ||
		ServiceAreaStatus_SERVICE_AREA_STATUS_OFFLINE != 2 {
		t.Fatal("ServiceAreaStatus values must match persisted service area statuses")
	}
}
