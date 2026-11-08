package v1

import (
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"

	_ "google.golang.org/genproto/googleapis/api/annotations"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
)

const (
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// 数值与 service_areas.status 保持一一对应，避免协议与数据库状态漂移。
type ServiceAreaStatus int32

const (
	ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED ServiceAreaStatus = 0
	ServiceAreaStatus_SERVICE_AREA_STATUS_OPERATING          ServiceAreaStatus = 1
	ServiceAreaStatus_SERVICE_AREA_STATUS_OFFLINE            ServiceAreaStatus = 2
)

// Enum value maps for ServiceAreaStatus.
var (
	ServiceAreaStatus_name = map[int32]string{
		0: "SERVICE_AREA_STATUS_TEMPORARILY_CLOSED",
		1: "SERVICE_AREA_STATUS_OPERATING",
		2: "SERVICE_AREA_STATUS_OFFLINE",
	}
	ServiceAreaStatus_value = map[string]int32{
		"SERVICE_AREA_STATUS_TEMPORARILY_CLOSED": 0,
		"SERVICE_AREA_STATUS_OPERATING":          1,
		"SERVICE_AREA_STATUS_OFFLINE":            2,
	}
)

func (x ServiceAreaStatus) Enum() *ServiceAreaStatus {
	p := new(ServiceAreaStatus)
	*p = x
	return p
}

func (x ServiceAreaStatus) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (ServiceAreaStatus) Descriptor() protoreflect.EnumDescriptor {
	return file_api_ridehailing_v1_service_area_proto_enumTypes[0].Descriptor()
}

func (ServiceAreaStatus) Type() protoreflect.EnumType {
	return &file_api_ridehailing_v1_service_area_proto_enumTypes[0]
}

func (x ServiceAreaStatus) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use ServiceAreaStatus.Descriptor instead.
func (ServiceAreaStatus) EnumDescriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{0}
}

type ServiceArea struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	Id              int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	AreaCode        string                 `protobuf:"bytes,2,opt,name=area_code,json=areaCode,proto3" json:"area_code,omitempty"`
	AreaName        string                 `protobuf:"bytes,3,opt,name=area_name,json=areaName,proto3" json:"area_name,omitempty"`
	CityCode        string                 `protobuf:"bytes,4,opt,name=city_code,json=cityCode,proto3" json:"city_code,omitempty"`
	CityName        string                 `protobuf:"bytes,5,opt,name=city_name,json=cityName,proto3" json:"city_name,omitempty"`
	BoundaryGeojson string                 `protobuf:"bytes,6,opt,name=boundary_geojson,json=boundaryGeojson,proto3" json:"boundary_geojson,omitempty"`
	ServiceNotice   string                 `protobuf:"bytes,7,opt,name=service_notice,json=serviceNotice,proto3" json:"service_notice,omitempty"`
	Status          ServiceAreaStatus      `protobuf:"varint,8,opt,name=status,proto3,enum=ridehailing.v1.ServiceAreaStatus" json:"status,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *ServiceArea) Reset() {
	*x = ServiceArea{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServiceArea) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServiceArea) ProtoMessage() {}

func (x *ServiceArea) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServiceArea.ProtoReflect.Descriptor instead.
func (*ServiceArea) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{0}
}

func (x *ServiceArea) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

func (x *ServiceArea) GetAreaCode() string {
	if x != nil {
		return x.AreaCode
	}
	return ""
}

func (x *ServiceArea) GetAreaName() string {
	if x != nil {
		return x.AreaName
	}
	return ""
}

func (x *ServiceArea) GetCityCode() string {
	if x != nil {
		return x.CityCode
	}
	return ""
}

func (x *ServiceArea) GetCityName() string {
	if x != nil {
		return x.CityName
	}
	return ""
}

func (x *ServiceArea) GetBoundaryGeojson() string {
	if x != nil {
		return x.BoundaryGeojson
	}
	return ""
}

func (x *ServiceArea) GetServiceNotice() string {
	if x != nil {
		return x.ServiceNotice
	}
	return ""
}

func (x *ServiceArea) GetStatus() ServiceAreaStatus {
	if x != nil {
		return x.Status
	}
	return ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED
}

type ServiceAreaSchedule struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	// 常规规则填 1-7（周一至周日）；特殊日期规则填 0 并提供 special_date。
	DayOfWeek int32 `protobuf:"varint,1,opt,name=day_of_week,json=dayOfWeek,proto3" json:"day_of_week,omitempty"`
	// YYYY-MM-DD；非空时覆盖同日的星期常规规则。
	SpecialDate string `protobuf:"bytes,2,opt,name=special_date,json=specialDate,proto3" json:"special_date,omitempty"`
	// 以 0-1439 的本地分钟表达；开始值大于结束值表示跨午夜。
	StartMinute   int32  `protobuf:"varint,3,opt,name=start_minute,json=startMinute,proto3" json:"start_minute,omitempty"`
	EndMinute     int32  `protobuf:"varint,4,opt,name=end_minute,json=endMinute,proto3" json:"end_minute,omitempty"`
	IsEnabled     bool   `protobuf:"varint,5,opt,name=is_enabled,json=isEnabled,proto3" json:"is_enabled,omitempty"`
	Notice        string `protobuf:"bytes,6,opt,name=notice,proto3" json:"notice,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ServiceAreaSchedule) Reset() {
	*x = ServiceAreaSchedule{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServiceAreaSchedule) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServiceAreaSchedule) ProtoMessage() {}

func (x *ServiceAreaSchedule) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServiceAreaSchedule.ProtoReflect.Descriptor instead.
func (*ServiceAreaSchedule) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{1}
}

func (x *ServiceAreaSchedule) GetDayOfWeek() int32 {
	if x != nil {
		return x.DayOfWeek
	}
	return 0
}

func (x *ServiceAreaSchedule) GetSpecialDate() string {
	if x != nil {
		return x.SpecialDate
	}
	return ""
}

func (x *ServiceAreaSchedule) GetStartMinute() int32 {
	if x != nil {
		return x.StartMinute
	}
	return 0
}

func (x *ServiceAreaSchedule) GetEndMinute() int32 {
	if x != nil {
		return x.EndMinute
	}
	return 0
}

func (x *ServiceAreaSchedule) GetIsEnabled() bool {
	if x != nil {
		return x.IsEnabled
	}
	return false
}

func (x *ServiceAreaSchedule) GetNotice() string {
	if x != nil {
		return x.Notice
	}
	return ""
}

type CreateServiceAreaRequest struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	AreaCode        string                 `protobuf:"bytes,1,opt,name=area_code,json=areaCode,proto3" json:"area_code,omitempty"`
	AreaName        string                 `protobuf:"bytes,2,opt,name=area_name,json=areaName,proto3" json:"area_name,omitempty"`
	CityCode        string                 `protobuf:"bytes,3,opt,name=city_code,json=cityCode,proto3" json:"city_code,omitempty"`
	CityName        string                 `protobuf:"bytes,4,opt,name=city_name,json=cityName,proto3" json:"city_name,omitempty"`
	BoundaryGeojson string                 `protobuf:"bytes,5,opt,name=boundary_geojson,json=boundaryGeojson,proto3" json:"boundary_geojson,omitempty"`
	ServiceNotice   string                 `protobuf:"bytes,6,opt,name=service_notice,json=serviceNotice,proto3" json:"service_notice,omitempty"`
	Status          ServiceAreaStatus      `protobuf:"varint,7,opt,name=status,proto3,enum=ridehailing.v1.ServiceAreaStatus" json:"status,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *CreateServiceAreaRequest) Reset() {
	*x = CreateServiceAreaRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CreateServiceAreaRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CreateServiceAreaRequest) ProtoMessage() {}

func (x *CreateServiceAreaRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CreateServiceAreaRequest.ProtoReflect.Descriptor instead.
func (*CreateServiceAreaRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{2}
}

func (x *CreateServiceAreaRequest) GetAreaCode() string {
	if x != nil {
		return x.AreaCode
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetAreaName() string {
	if x != nil {
		return x.AreaName
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetCityCode() string {
	if x != nil {
		return x.CityCode
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetCityName() string {
	if x != nil {
		return x.CityName
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetBoundaryGeojson() string {
	if x != nil {
		return x.BoundaryGeojson
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetServiceNotice() string {
	if x != nil {
		return x.ServiceNotice
	}
	return ""
}

func (x *CreateServiceAreaRequest) GetStatus() ServiceAreaStatus {
	if x != nil {
		return x.Status
	}
	return ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED
}

type ListServiceAreasRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Page          int32                  `protobuf:"varint,1,opt,name=page,proto3" json:"page,omitempty"`
	PageSize      int32                  `protobuf:"varint,2,opt,name=page_size,json=pageSize,proto3" json:"page_size,omitempty"`
	CityCode      string                 `protobuf:"bytes,3,opt,name=city_code,json=cityCode,proto3" json:"city_code,omitempty"`
	Status        *ServiceAreaStatus     `protobuf:"varint,4,opt,name=status,proto3,enum=ridehailing.v1.ServiceAreaStatus,oneof" json:"status,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListServiceAreasRequest) Reset() {
	*x = ListServiceAreasRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListServiceAreasRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListServiceAreasRequest) ProtoMessage() {}

func (x *ListServiceAreasRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListServiceAreasRequest.ProtoReflect.Descriptor instead.
func (*ListServiceAreasRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{3}
}

func (x *ListServiceAreasRequest) GetPage() int32 {
	if x != nil {
		return x.Page
	}
	return 0
}

func (x *ListServiceAreasRequest) GetPageSize() int32 {
	if x != nil {
		return x.PageSize
	}
	return 0
}

func (x *ListServiceAreasRequest) GetCityCode() string {
	if x != nil {
		return x.CityCode
	}
	return ""
}

func (x *ListServiceAreasRequest) GetStatus() ServiceAreaStatus {
	if x != nil && x.Status != nil {
		return *x.Status
	}
	return ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED
}

type GetServiceAreaRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *GetServiceAreaRequest) Reset() {
	*x = GetServiceAreaRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *GetServiceAreaRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*GetServiceAreaRequest) ProtoMessage() {}

func (x *GetServiceAreaRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use GetServiceAreaRequest.ProtoReflect.Descriptor instead.
func (*GetServiceAreaRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{4}
}

func (x *GetServiceAreaRequest) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

// area_code 不可变，故更新请求中不提供该字段。
type UpdateServiceAreaRequest struct {
	state           protoimpl.MessageState `protogen:"open.v1"`
	Id              int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	AreaName        string                 `protobuf:"bytes,2,opt,name=area_name,json=areaName,proto3" json:"area_name,omitempty"`
	CityCode        string                 `protobuf:"bytes,3,opt,name=city_code,json=cityCode,proto3" json:"city_code,omitempty"`
	CityName        string                 `protobuf:"bytes,4,opt,name=city_name,json=cityName,proto3" json:"city_name,omitempty"`
	BoundaryGeojson string                 `protobuf:"bytes,5,opt,name=boundary_geojson,json=boundaryGeojson,proto3" json:"boundary_geojson,omitempty"`
	ServiceNotice   string                 `protobuf:"bytes,6,opt,name=service_notice,json=serviceNotice,proto3" json:"service_notice,omitempty"`
	unknownFields   protoimpl.UnknownFields
	sizeCache       protoimpl.SizeCache
}

func (x *UpdateServiceAreaRequest) Reset() {
	*x = UpdateServiceAreaRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *UpdateServiceAreaRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UpdateServiceAreaRequest) ProtoMessage() {}

func (x *UpdateServiceAreaRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use UpdateServiceAreaRequest.ProtoReflect.Descriptor instead.
func (*UpdateServiceAreaRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{5}
}

func (x *UpdateServiceAreaRequest) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

func (x *UpdateServiceAreaRequest) GetAreaName() string {
	if x != nil {
		return x.AreaName
	}
	return ""
}

func (x *UpdateServiceAreaRequest) GetCityCode() string {
	if x != nil {
		return x.CityCode
	}
	return ""
}

func (x *UpdateServiceAreaRequest) GetCityName() string {
	if x != nil {
		return x.CityName
	}
	return ""
}

func (x *UpdateServiceAreaRequest) GetBoundaryGeojson() string {
	if x != nil {
		return x.BoundaryGeojson
	}
	return ""
}

func (x *UpdateServiceAreaRequest) GetServiceNotice() string {
	if x != nil {
		return x.ServiceNotice
	}
	return ""
}

type ChangeServiceAreaStatusRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Id            int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	Status        ServiceAreaStatus      `protobuf:"varint,2,opt,name=status,proto3,enum=ridehailing.v1.ServiceAreaStatus" json:"status,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ChangeServiceAreaStatusRequest) Reset() {
	*x = ChangeServiceAreaStatusRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ChangeServiceAreaStatusRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ChangeServiceAreaStatusRequest) ProtoMessage() {}

func (x *ChangeServiceAreaStatusRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ChangeServiceAreaStatusRequest.ProtoReflect.Descriptor instead.
func (*ChangeServiceAreaStatusRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{6}
}

func (x *ChangeServiceAreaStatusRequest) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

func (x *ChangeServiceAreaStatusRequest) GetStatus() ServiceAreaStatus {
	if x != nil {
		return x.Status
	}
	return ServiceAreaStatus_SERVICE_AREA_STATUS_TEMPORARILY_CLOSED
}

type ReplaceServiceAreaSchedulesRequest struct {
	state protoimpl.MessageState `protogen:"open.v1"`
	Id    int64                  `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	// 空列表合法，表示该区域无可运营时段。
	Schedules     []*ServiceAreaSchedule `protobuf:"bytes,2,rep,name=schedules,proto3" json:"schedules,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ReplaceServiceAreaSchedulesRequest) Reset() {
	*x = ReplaceServiceAreaSchedulesRequest{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ReplaceServiceAreaSchedulesRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ReplaceServiceAreaSchedulesRequest) ProtoMessage() {}

func (x *ReplaceServiceAreaSchedulesRequest) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ReplaceServiceAreaSchedulesRequest.ProtoReflect.Descriptor instead.
func (*ReplaceServiceAreaSchedulesRequest) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{7}
}

func (x *ReplaceServiceAreaSchedulesRequest) GetId() int64 {
	if x != nil {
		return x.Id
	}
	return 0
}

func (x *ReplaceServiceAreaSchedulesRequest) GetSchedules() []*ServiceAreaSchedule {
	if x != nil {
		return x.Schedules
	}
	return nil
}

type ServiceAreaReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ServiceArea   *ServiceArea           `protobuf:"bytes,1,opt,name=service_area,json=serviceArea,proto3" json:"service_area,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ServiceAreaReply) Reset() {
	*x = ServiceAreaReply{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[8]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServiceAreaReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServiceAreaReply) ProtoMessage() {}

func (x *ServiceAreaReply) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[8]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServiceAreaReply.ProtoReflect.Descriptor instead.
func (*ServiceAreaReply) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{8}
}

func (x *ServiceAreaReply) GetServiceArea() *ServiceArea {
	if x != nil {
		return x.ServiceArea
	}
	return nil
}

type ListServiceAreasReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ServiceAreas  []*ServiceArea         `protobuf:"bytes,1,rep,name=service_areas,json=serviceAreas,proto3" json:"service_areas,omitempty"`
	Total         int64                  `protobuf:"varint,2,opt,name=total,proto3" json:"total,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListServiceAreasReply) Reset() {
	*x = ListServiceAreasReply{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[9]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListServiceAreasReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListServiceAreasReply) ProtoMessage() {}

func (x *ListServiceAreasReply) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[9]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListServiceAreasReply.ProtoReflect.Descriptor instead.
func (*ListServiceAreasReply) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{9}
}

func (x *ListServiceAreasReply) GetServiceAreas() []*ServiceArea {
	if x != nil {
		return x.ServiceAreas
	}
	return nil
}

func (x *ListServiceAreasReply) GetTotal() int64 {
	if x != nil {
		return x.Total
	}
	return 0
}

type ServiceAreaDetailReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	ServiceArea   *ServiceArea           `protobuf:"bytes,1,opt,name=service_area,json=serviceArea,proto3" json:"service_area,omitempty"`
	Schedules     []*ServiceAreaSchedule `protobuf:"bytes,2,rep,name=schedules,proto3" json:"schedules,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ServiceAreaDetailReply) Reset() {
	*x = ServiceAreaDetailReply{}
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[10]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ServiceAreaDetailReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ServiceAreaDetailReply) ProtoMessage() {}

func (x *ServiceAreaDetailReply) ProtoReflect() protoreflect.Message {
	mi := &file_api_ridehailing_v1_service_area_proto_msgTypes[10]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ServiceAreaDetailReply.ProtoReflect.Descriptor instead.
func (*ServiceAreaDetailReply) Descriptor() ([]byte, []int) {
	return file_api_ridehailing_v1_service_area_proto_rawDescGZIP(), []int{10}
}

func (x *ServiceAreaDetailReply) GetServiceArea() *ServiceArea {
	if x != nil {
		return x.ServiceArea
	}
	return nil
}

func (x *ServiceAreaDetailReply) GetSchedules() []*ServiceAreaSchedule {
	if x != nil {
		return x.Schedules
	}
	return nil
}

var File_api_ridehailing_v1_service_area_proto protoreflect.FileDescriptor

const file_api_ridehailing_v1_service_area_proto_rawDesc = "" +
	"\n" +
	"%api/ridehailing/v1/service_area.proto\x12\x0eridehailing.v1\x1a\x1cgoogle/api/annotations.proto\"\x9e\x02\n" +
	"\vServiceArea\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\x03R\x02id\x12\x1b\n" +
	"\tarea_code\x18\x02 \x01(\tR\bareaCode\x12\x1b\n" +
	"\tarea_name\x18\x03 \x01(\tR\bareaName\x12\x1b\n" +
	"\tcity_code\x18\x04 \x01(\tR\bcityCode\x12\x1b\n" +
	"\tcity_name\x18\x05 \x01(\tR\bcityName\x12)\n" +
	"\x10boundary_geojson\x18\x06 \x01(\tR\x0fboundaryGeojson\x12%\n" +
	"\x0eservice_notice\x18\a \x01(\tR\rserviceNotice\x129\n" +
	"\x06status\x18\b \x01(\x0e2!.ridehailing.v1.ServiceAreaStatusR\x06status\"\xd1\x01\n" +
	"\x13ServiceAreaSchedule\x12\x1e\n" +
	"\vday_of_week\x18\x01 \x01(\x05R\tdayOfWeek\x12!\n" +
	"\fspecial_date\x18\x02 \x01(\tR\vspecialDate\x12!\n" +
	"\fstart_minute\x18\x03 \x01(\x05R\vstartMinute\x12\x1d\n" +
	"\n" +
	"end_minute\x18\x04 \x01(\x05R\tendMinute\x12\x1d\n" +
	"\n" +
	"is_enabled\x18\x05 \x01(\bR\tisEnabled\x12\x16\n" +
	"\x06notice\x18\x06 \x01(\tR\x06notice\"\x9b\x02\n" +
	"\x18CreateServiceAreaRequest\x12\x1b\n" +
	"\tarea_code\x18\x01 \x01(\tR\bareaCode\x12\x1b\n" +
	"\tarea_name\x18\x02 \x01(\tR\bareaName\x12\x1b\n" +
	"\tcity_code\x18\x03 \x01(\tR\bcityCode\x12\x1b\n" +
	"\tcity_name\x18\x04 \x01(\tR\bcityName\x12)\n" +
	"\x10boundary_geojson\x18\x05 \x01(\tR\x0fboundaryGeojson\x12%\n" +
	"\x0eservice_notice\x18\x06 \x01(\tR\rserviceNotice\x129\n" +
	"\x06status\x18\a \x01(\x0e2!.ridehailing.v1.ServiceAreaStatusR\x06status\"\xb2\x01\n" +
	"\x17ListServiceAreasRequest\x12\x12\n" +
	"\x04page\x18\x01 \x01(\x05R\x04page\x12\x1b\n" +
	"\tpage_size\x18\x02 \x01(\x05R\bpageSize\x12\x1b\n" +
	"\tcity_code\x18\x03 \x01(\tR\bcityCode\x12>\n" +
	"\x06status\x18\x04 \x01(\x0e2!.ridehailing.v1.ServiceAreaStatusH\x00R\x06status\x88\x01\x01B\t\n" +
	"\a_status\"'\n" +
	"\x15GetServiceAreaRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\x03R\x02id\"\xd3\x01\n" +
	"\x18UpdateServiceAreaRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\x03R\x02id\x12\x1b\n" +
	"\tarea_name\x18\x02 \x01(\tR\bareaName\x12\x1b\n" +
	"\tcity_code\x18\x03 \x01(\tR\bcityCode\x12\x1b\n" +
	"\tcity_name\x18\x04 \x01(\tR\bcityName\x12)\n" +
	"\x10boundary_geojson\x18\x05 \x01(\tR\x0fboundaryGeojson\x12%\n" +
	"\x0eservice_notice\x18\x06 \x01(\tR\rserviceNotice\"k\n" +
	"\x1eChangeServiceAreaStatusRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\x03R\x02id\x129\n" +
	"\x06status\x18\x02 \x01(\x0e2!.ridehailing.v1.ServiceAreaStatusR\x06status\"w\n" +
	"\"ReplaceServiceAreaSchedulesRequest\x12\x0e\n" +
	"\x02id\x18\x01 \x01(\x03R\x02id\x12A\n" +
	"\tschedules\x18\x02 \x03(\v2#.ridehailing.v1.ServiceAreaScheduleR\tschedules\"R\n" +
	"\x10ServiceAreaReply\x12>\n" +
	"\fservice_area\x18\x01 \x01(\v2\x1b.ridehailing.v1.ServiceAreaR\vserviceArea\"o\n" +
	"\x15ListServiceAreasReply\x12@\n" +
	"\rservice_areas\x18\x01 \x03(\v2\x1b.ridehailing.v1.ServiceAreaR\fserviceAreas\x12\x14\n" +
	"\x05total\x18\x02 \x01(\x03R\x05total\"\x9b\x01\n" +
	"\x16ServiceAreaDetailReply\x12>\n" +
	"\fservice_area\x18\x01 \x01(\v2\x1b.ridehailing.v1.ServiceAreaR\vserviceArea\x12A\n" +
	"\tschedules\x18\x02 \x03(\v2#.ridehailing.v1.ServiceAreaScheduleR\tschedules*\x83\x01\n" +
	"\x11ServiceAreaStatus\x12*\n" +
	"&SERVICE_AREA_STATUS_TEMPORARILY_CLOSED\x10\x00\x12!\n" +
	"\x1dSERVICE_AREA_STATUS_OPERATING\x10\x01\x12\x1f\n" +
	"\x1bSERVICE_AREA_STATUS_OFFLINE\x10\x022\x85\a\n" +
	"\x17ServiceAreaAdminService\x12\x83\x01\n" +
	"\x11CreateServiceArea\x12(.ridehailing.v1.CreateServiceAreaRequest\x1a .ridehailing.v1.ServiceAreaReply\"\"\x82\xd3\xe4\x93\x02\x1c:\x01*\"\x17/v1/admin/service-areas\x12\x83\x01\n" +
	"\x10ListServiceAreas\x12'.ridehailing.v1.ListServiceAreasRequest\x1a%.ridehailing.v1.ListServiceAreasReply\"\x1f\x82\xd3\xe4\x93\x02\x19\x12\x17/v1/admin/service-areas\x12\x85\x01\n" +
	"\x0eGetServiceArea\x12%.ridehailing.v1.GetServiceAreaRequest\x1a&.ridehailing.v1.ServiceAreaDetailReply\"$\x82\xd3\xe4\x93\x02\x1e\x12\x1c/v1/admin/service-areas/{id}\x12\x88\x01\n" +
	"\x11UpdateServiceArea\x12(.ridehailing.v1.UpdateServiceAreaRequest\x1a .ridehailing.v1.ServiceAreaReply\"'\x82\xd3\xe4\x93\x02!:\x01*2\x1c/v1/admin/service-areas/{id}\x12\x9b\x01\n" +
	"\x17ChangeServiceAreaStatus\x12..ridehailing.v1.ChangeServiceAreaStatusRequest\x1a .ridehailing.v1.ServiceAreaReply\".\x82\xd3\xe4\x93\x02(:\x01*2#/v1/admin/service-areas/{id}/status\x12\xac\x01\n" +
	"\x1bReplaceServiceAreaSchedules\x122.ridehailing.v1.ReplaceServiceAreaSchedulesRequest\x1a&.ridehailing.v1.ServiceAreaDetailReply\"1\x82\xd3\xe4\x93\x02+:\x01*\x1a&/v1/admin/service-areas/{id}/schedulesBCZAgithub.com/lijayin-0202/radish-ride-service/api/ridehailing/v1;v1b\x06proto3"

var (
	file_api_ridehailing_v1_service_area_proto_rawDescOnce sync.Once
	file_api_ridehailing_v1_service_area_proto_rawDescData []byte
)

func file_api_ridehailing_v1_service_area_proto_rawDescGZIP() []byte {
	file_api_ridehailing_v1_service_area_proto_rawDescOnce.Do(func() {
		file_api_ridehailing_v1_service_area_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_api_ridehailing_v1_service_area_proto_rawDesc), len(file_api_ridehailing_v1_service_area_proto_rawDesc)))
	})
	return file_api_ridehailing_v1_service_area_proto_rawDescData
}

var file_api_ridehailing_v1_service_area_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_api_ridehailing_v1_service_area_proto_msgTypes = make([]protoimpl.MessageInfo, 11)
var file_api_ridehailing_v1_service_area_proto_goTypes = []any{
	(ServiceAreaStatus)(0),                     // 0: ridehailing.v1.ServiceAreaStatus
	(*ServiceArea)(nil),                        // 1: ridehailing.v1.ServiceArea
	(*ServiceAreaSchedule)(nil),                // 2: ridehailing.v1.ServiceAreaSchedule
	(*CreateServiceAreaRequest)(nil),           // 3: ridehailing.v1.CreateServiceAreaRequest
	(*ListServiceAreasRequest)(nil),            // 4: ridehailing.v1.ListServiceAreasRequest
	(*GetServiceAreaRequest)(nil),              // 5: ridehailing.v1.GetServiceAreaRequest
	(*UpdateServiceAreaRequest)(nil),           // 6: ridehailing.v1.UpdateServiceAreaRequest
	(*ChangeServiceAreaStatusRequest)(nil),     // 7: ridehailing.v1.ChangeServiceAreaStatusRequest
	(*ReplaceServiceAreaSchedulesRequest)(nil), // 8: ridehailing.v1.ReplaceServiceAreaSchedulesRequest
	(*ServiceAreaReply)(nil),                   // 9: ridehailing.v1.ServiceAreaReply
	(*ListServiceAreasReply)(nil),              // 10: ridehailing.v1.ListServiceAreasReply
	(*ServiceAreaDetailReply)(nil),             // 11: ridehailing.v1.ServiceAreaDetailReply
}
var file_api_ridehailing_v1_service_area_proto_depIdxs = []int32{
	0,  // 0: ridehailing.v1.ServiceArea.status:type_name -> ridehailing.v1.ServiceAreaStatus
	0,  // 1: ridehailing.v1.CreateServiceAreaRequest.status:type_name -> ridehailing.v1.ServiceAreaStatus
	0,  // 2: ridehailing.v1.ListServiceAreasRequest.status:type_name -> ridehailing.v1.ServiceAreaStatus
	0,  // 3: ridehailing.v1.ChangeServiceAreaStatusRequest.status:type_name -> ridehailing.v1.ServiceAreaStatus
	2,  // 4: ridehailing.v1.ReplaceServiceAreaSchedulesRequest.schedules:type_name -> ridehailing.v1.ServiceAreaSchedule
	1,  // 5: ridehailing.v1.ServiceAreaReply.service_area:type_name -> ridehailing.v1.ServiceArea
	1,  // 6: ridehailing.v1.ListServiceAreasReply.service_areas:type_name -> ridehailing.v1.ServiceArea
	1,  // 7: ridehailing.v1.ServiceAreaDetailReply.service_area:type_name -> ridehailing.v1.ServiceArea
	2,  // 8: ridehailing.v1.ServiceAreaDetailReply.schedules:type_name -> ridehailing.v1.ServiceAreaSchedule
	3,  // 9: ridehailing.v1.ServiceAreaAdminService.CreateServiceArea:input_type -> ridehailing.v1.CreateServiceAreaRequest
	4,  // 10: ridehailing.v1.ServiceAreaAdminService.ListServiceAreas:input_type -> ridehailing.v1.ListServiceAreasRequest
	5,  // 11: ridehailing.v1.ServiceAreaAdminService.GetServiceArea:input_type -> ridehailing.v1.GetServiceAreaRequest
	6,  // 12: ridehailing.v1.ServiceAreaAdminService.UpdateServiceArea:input_type -> ridehailing.v1.UpdateServiceAreaRequest
	7,  // 13: ridehailing.v1.ServiceAreaAdminService.ChangeServiceAreaStatus:input_type -> ridehailing.v1.ChangeServiceAreaStatusRequest
	8,  // 14: ridehailing.v1.ServiceAreaAdminService.ReplaceServiceAreaSchedules:input_type -> ridehailing.v1.ReplaceServiceAreaSchedulesRequest
	9,  // 15: ridehailing.v1.ServiceAreaAdminService.CreateServiceArea:output_type -> ridehailing.v1.ServiceAreaReply
	10, // 16: ridehailing.v1.ServiceAreaAdminService.ListServiceAreas:output_type -> ridehailing.v1.ListServiceAreasReply
	11, // 17: ridehailing.v1.ServiceAreaAdminService.GetServiceArea:output_type -> ridehailing.v1.ServiceAreaDetailReply
	9,  // 18: ridehailing.v1.ServiceAreaAdminService.UpdateServiceArea:output_type -> ridehailing.v1.ServiceAreaReply
	9,  // 19: ridehailing.v1.ServiceAreaAdminService.ChangeServiceAreaStatus:output_type -> ridehailing.v1.ServiceAreaReply
	11, // 20: ridehailing.v1.ServiceAreaAdminService.ReplaceServiceAreaSchedules:output_type -> ridehailing.v1.ServiceAreaDetailReply
	15, // [15:21] is the sub-list for method output_type
	9,  // [9:15] is the sub-list for method input_type
	9,  // [9:9] is the sub-list for extension type_name
	9,  // [9:9] is the sub-list for extension extendee
	0,  // [0:9] is the sub-list for field type_name
}

func init() { file_api_ridehailing_v1_service_area_proto_init() }
func file_api_ridehailing_v1_service_area_proto_init() {
	if File_api_ridehailing_v1_service_area_proto != nil {
		return
	}
	file_api_ridehailing_v1_service_area_proto_msgTypes[3].OneofWrappers = []any{}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_api_ridehailing_v1_service_area_proto_rawDesc), len(file_api_ridehailing_v1_service_area_proto_rawDesc)),
			NumEnums:      1,
			NumMessages:   11,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_api_ridehailing_v1_service_area_proto_goTypes,
		DependencyIndexes: file_api_ridehailing_v1_service_area_proto_depIdxs,
		EnumInfos:         file_api_ridehailing_v1_service_area_proto_enumTypes,
		MessageInfos:      file_api_ridehailing_v1_service_area_proto_msgTypes,
	}.Build()
	File_api_ridehailing_v1_service_area_proto = out.File
	file_api_ridehailing_v1_service_area_proto_goTypes = nil
	file_api_ridehailing_v1_service_area_proto_depIdxs = nil
}
