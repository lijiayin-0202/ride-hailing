// 模块1 用户账号与个人中心（乘客端）—— 成员A
// 接口层职责：参数绑定与校验、登录态提取、调用 biz 用例、组织返回结构。
// 本层禁止写 SQL、禁止直接访问 data 层（AGENT.md §7、§4）。
package service

import (
	"context"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	v1 "github.com/lijayin-0202/radish-ride-service/api/ridehailing/v1"
	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/pkg/device"
	"github.com/lijayin-0202/radish-ride-service/pkg/iputil"
	"github.com/lijayin-0202/radish-ride-service/pkg/mask"
)

// cstZone 业务时区（北京时间），响应时间统一带 +08:00 偏移（前端接口文档 §1.1 ISO 8601）。
var cstZone = time.FixedZone("CST", 8*3600)

// PassengerAccountService 实现 v1.PassengerAccountServer。
type PassengerAccountService struct {
	v1.UnimplementedPassengerAccountServer

	// authUC 登录/注册、验证码与令牌校验。
	authUC *biz.AuthUsecase
	// passengerUC 个人中心聚合与偏好设置。
	passengerUC *biz.PassengerUsecase
	// addressUC 常用地址增删改查。
	addressUC *biz.AddressUsecase
	// couponUC 券包查询。
	couponUC *biz.CouponUsecase
	// pointUC 积分流水与签到。
	pointUC *biz.PointUsecase
	// notificationUC 消息列表与已读标记。
	notificationUC *biz.NotificationUsecase
	// realNameUC 实名认证提交与查询。
	realNameUC *biz.RealNameUsecase
	// loginDeviceUC 登录设备登记、设备列表与登录记录查询。
	loginDeviceUC *biz.LoginDeviceUsecase

	// log 结构化日志；本层禁止打印 SQL 与敏感明文。
	log *log.Helper
}

// NewPassengerAccountService 构造函数（wire 注入）。
func NewPassengerAccountService(
	authUC *biz.AuthUsecase,
	passengerUC *biz.PassengerUsecase,
	addressUC *biz.AddressUsecase,
	couponUC *biz.CouponUsecase,
	pointUC *biz.PointUsecase,
	notificationUC *biz.NotificationUsecase,
	realNameUC *biz.RealNameUsecase,
	loginDeviceUC *biz.LoginDeviceUsecase,
	logger log.Logger,
) *PassengerAccountService {
	return &PassengerAccountService{
		authUC:         authUC,
		passengerUC:    passengerUC,
		addressUC:      addressUC,
		couponUC:       couponUC,
		pointUC:        pointUC,
		notificationUC: notificationUC,
		realNameUC:     realNameUC,
		loginDeviceUC:  loginDeviceUC,
		log:            log.NewHelper(log.With(logger, "module", "service/passenger_account")),
	}
}

// ---------------------------- 登录 ----------------------------

// SendLoginSmsCode 发送验证码。
// scene = LOGIN 时无需登录；换绑手机号的两个场景必须携带登录态（用于校验原号归属与新号可用性）。
func (s *PassengerAccountService) SendLoginSmsCode(ctx context.Context, req *v1.SendLoginSmsCodeRequest) (*v1.SendLoginSmsCodeReply, error) {
	scene := strings.ToUpper(strings.TrimSpace(req.GetScene()))
	var passengerID int64
	if scene != "" && scene != biz.SmsCodeSceneLogin {
		id, err := s.currentPassengerID(ctx)
		if err != nil {
			return nil, err
		}
		passengerID = id
	}
	ticket, err := s.authUC.SendSmsCode(ctx, passengerID, req.GetMobile(), scene)
	if err != nil {
		return nil, err
	}
	return &v1.SendLoginSmsCodeReply{
		VerificationId:     ticket.VerificationID,
		ExpiresInSeconds:   ticket.ExpiresInSeconds,
		ResendAfterSeconds: ticket.ResendAfterSeconds,
		MaskedMobile:       ticket.MaskedMobile,
	}, nil
}

// MobileLogin 手机号验证码登录/注册（无需登录态）。
func (s *PassengerAccountService) MobileLogin(ctx context.Context, req *v1.MobileLoginRequest) (*v1.MobileLoginReply, error) {
	ip, ua, _ := clientMeta(ctx)
	// 设备信息从 User-Agent 解析，归属地按 IP 兜底解析（未接入 IP 库时返回内网/未知归属地）。
	deviceInfo := device.Parse(ua)
	location := iputil.ResolveLocation(ip)

	result, err := s.authUC.MobileLogin(ctx, &biz.LoginInput{
		Mobile:         req.GetMobile(),
		SmsCode:        req.GetSmsCode(),
		VerificationID: req.GetVerificationId(),
		DeviceID:       req.GetDeviceId(),
		DeviceName:     deviceInfo.Name,
		DeviceModel:    deviceInfo.Model,
		OSName:         deviceInfo.OSName,
		OSVersion:      deviceInfo.OSVersion,
		AppVersion:     clientVersion(ctx),
		ClientIP:       ip,
		LoginLocation:  location,
		UserAgent:      ua,
	})
	if err != nil {
		return nil, err
	}

	// 登记登录设备（账号管理页「登录设备」）。失败不影响登录主流程，仅记录告警。
	if err := s.loginDeviceUC.RecordLogin(ctx, result.Passenger.ID, &biz.DeviceLoginInfo{
		DeviceID:    req.GetDeviceId(),
		DeviceName:  deviceInfo.Name,
		DeviceModel: deviceInfo.Model,
		OSName:      deviceInfo.OSName,
		OSVersion:   deviceInfo.OSVersion,
		AppVersion:  clientVersion(ctx),
		IP:          ip,
		Location:    location,
	}); err != nil {
		// RecordLogin 内部已记录告警（含 device_id），这里不再重复打印，也不影响登录结果。
		_ = err
	}
	return &v1.MobileLoginReply{
		Passenger:        toPassengerSummary(result.Passenger),
		AccessToken:      result.AccessToken,
		ExpiresInSeconds: result.ExpiresIn,
		IsNewUser:        result.IsNewUser,
	}, nil
}

// ---------------------------- 个人中心 ----------------------------

// GetProfile 个人中心聚合查询。
func (s *PassengerAccountService) GetProfile(ctx context.Context, _ *v1.GetProfileRequest) (*v1.GetProfileReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := s.passengerUC.GetProfile(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return &v1.GetProfileReply{
		Passenger:               toPassengerSummary(profile.Passenger),
		WalletBalance:           profile.WalletBalance,
		AvailableCouponCount:    profile.AvailableCouponCount,
		UnreadNotificationCount: profile.UnreadNotificationCount,
		SignedInToday:           profile.SignedInToday,
		ContinuousSignInDays:    profile.ContinuousSignInDays,
	}, nil
}

// UpdateNickname 修改用户名（昵称）。
func (s *PassengerAccountService) UpdateNickname(ctx context.Context, req *v1.UpdateNicknameRequest) (*v1.UpdateNicknameReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	passenger, err := s.passengerUC.UpdateNickname(ctx, passengerID, req.GetNickname())
	if err != nil {
		return nil, err
	}
	return &v1.UpdateNicknameReply{Passenger: toPassengerSummary(passenger)}, nil
}

// ChangeMobile 更换绑定手机号（原号 + 新号双验证）。
func (s *PassengerAccountService) ChangeMobile(ctx context.Context, req *v1.ChangeMobileRequest) (*v1.ChangeMobileReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	ip, ua, _ := clientMeta(ctx)
	deviceInfo := device.Parse(ua)

	passenger, err := s.authUC.ChangeMobile(ctx, passengerID, &biz.ChangeMobileInput{
		NewMobile:         req.GetNewMobile(),
		OldSmsCode:        req.GetOldSmsCode(),
		OldVerificationID: req.GetOldVerificationId(),
		NewSmsCode:        req.GetNewSmsCode(),
		NewVerificationID: req.GetNewVerificationId(),
		DeviceName:        deviceInfo.Name,
		DeviceModel:       deviceInfo.Model,
		ClientIP:          ip,
		LoginLocation:     iputil.ResolveLocation(ip),
		UserAgent:         ua,
	})
	if err != nil {
		return nil, err
	}
	return &v1.ChangeMobileReply{Passenger: toPassengerSummary(passenger)}, nil
}

// UpdateSettings 偏好设置（通知/隐私/显示）。
func (s *PassengerAccountService) UpdateSettings(ctx context.Context, req *v1.UpdateSettingsRequest) (*v1.UpdateSettingsReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	pref := biz.PassengerPreferences{}
	if in := req.GetPreferences(); in != nil {
		pref.OrderNotificationEnabled = in.OrderNotificationEnabled
		pref.PromotionNotificationEnabled = in.PromotionNotificationEnabled
		pref.SecurityNotificationEnabled = in.SecurityNotificationEnabled
		pref.TripShareDefaultEnabled = in.TripShareDefaultEnabled
	}
	merged, err := s.passengerUC.UpdatePreferences(ctx, passengerID, pref)
	if err != nil {
		return nil, err
	}
	return &v1.UpdateSettingsReply{Preferences: toPreferences(merged)}, nil
}

// ---------------------------- 常用地址 ----------------------------

// ListAddresses 常用地址分页列表。
func (s *PassengerAccountService) ListAddresses(ctx context.Context, req *v1.ListAddressesRequest) (*v1.ListAddressesReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.addressUC.List(ctx, passengerID, req.GetPage(), req.GetPageSize(), req.AddressType)
	if err != nil {
		return nil, err
	}
	reply := &v1.ListAddressesReply{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Items:    make([]*v1.PassengerAddress, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		reply.Items = append(reply.Items, toProtoAddress(item))
	}
	return reply, nil
}

// CreateAddress 新增常用地址。
func (s *PassengerAccountService) CreateAddress(ctx context.Context, req *v1.CreateAddressRequest) (*v1.AddressReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	addressType := req.GetAddressType()
	name := req.GetAddressName()
	detail := req.GetAddress()
	longitude := req.GetLongitude()
	latitude := req.GetLatitude()
	stationID := req.GetStationId()
	isDefault := req.GetIsDefault()

	address, err := s.addressUC.Create(ctx, passengerID, &biz.AddressInput{
		AddressType: &addressType,
		AddressName: &name,
		Address:     &detail,
		Longitude:   &longitude,
		Latitude:    &latitude,
		StationID:   &stationID,
		IsDefault:   &isDefault,
	})
	if err != nil {
		return nil, err
	}
	return &v1.AddressReply{Address: toProtoAddress(address)}, nil
}

// UpdateAddress 修改常用地址（PATCH 语义，仅更新传入字段）。
func (s *PassengerAccountService) UpdateAddress(ctx context.Context, req *v1.UpdateAddressRequest) (*v1.AddressReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	address, err := s.addressUC.Update(ctx, passengerID, req.GetId(), &biz.AddressInput{
		AddressType: req.AddressType,
		AddressName: req.AddressName,
		Address:     req.Address,
		Longitude:   req.Longitude,
		Latitude:    req.Latitude,
		StationID:   req.StationId,
		IsDefault:   req.IsDefault,
	})
	if err != nil {
		return nil, err
	}
	return &v1.AddressReply{Address: toProtoAddress(address)}, nil
}

// DeleteAddress 删除常用地址。
func (s *PassengerAccountService) DeleteAddress(ctx context.Context, req *v1.DeleteAddressRequest) (*v1.DeleteAddressReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.addressUC.Delete(ctx, passengerID, req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteAddressReply{Id: req.GetId(), Deleted: true}, nil
}

// ---------------------------- 优惠券 ----------------------------

// ListCoupons 我的优惠券聚合查询。
func (s *PassengerAccountService) ListCoupons(ctx context.Context, req *v1.ListCouponsRequest) (*v1.ListCouponsReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.couponUC.List(ctx, passengerID, req.GetPage(), req.GetPageSize(), req.Status)
	if err != nil {
		return nil, err
	}
	reply := &v1.ListCouponsReply{
		Total:          result.Total,
		Page:           result.Page,
		PageSize:       result.PageSize,
		AvailableCount: result.AvailableCount,
		Items:          make([]*v1.PassengerCoupon, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		coupon := &v1.PassengerCoupon{
			Id:                 item.ID,
			CouponId:           item.CouponID,
			CouponInstanceNo:   item.CouponInstanceNo,
			CouponName:         item.CouponName,
			CouponType:         item.CouponType,
			DiscountAmount:     item.DiscountAmount,
			DiscountRate:       item.DiscountRate,
			MinimumOrderAmount: item.MinimumOrderAmount,
			ValidFrom:          formatTime(item.ValidFrom),
			ValidTo:            formatTime(item.ValidTo),
			Status:             item.Status,
			ReceivedAt:         formatTime(item.ReceivedAt),
			ExpiresAt:          formatTime(item.ExpiresAt),
		}
		// 区域为 NULL 表示不限区域，proto 中保持 0 值。
		if item.ServiceAreaID != nil {
			coupon.ServiceAreaId = *item.ServiceAreaID
		}
		reply.Items = append(reply.Items, coupon)
	}
	return reply, nil
}

// ---------------------------- 积分与签到 ----------------------------

// GetPoints 积分余额与流水分页。
func (s *PassengerAccountService) GetPoints(ctx context.Context, req *v1.GetPointsRequest) (*v1.GetPointsReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.pointUC.GetPoints(ctx, passengerID, req.GetPage(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	reply := &v1.GetPointsReply{
		PointBalance: result.PointBalance,
		Total:        result.Total,
		Page:         result.Page,
		PageSize:     result.PageSize,
		Items:        make([]*v1.PointTransaction, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		transaction := &v1.PointTransaction{
			Id:            item.ID,
			TransactionNo: item.TransactionNo,
			ChangeAmount:  item.ChangeAmount,
			BalanceAfter:  item.BalanceAfter,
			SourceType:    item.SourceType,
			Remark:        item.Remark,
			CreatedAt:     formatTime(item.CreatedAt),
		}
		// 关联订单可空（签到/人工调整无订单），NULL 时保持 0 值。
		if item.RelatedOrderID != nil {
			transaction.RelatedOrderId = *item.RelatedOrderID
		}
		reply.Items = append(reply.Items, transaction)
	}
	return reply, nil
}

// SignIn 每日签到。
func (s *PassengerAccountService) SignIn(ctx context.Context, _ *v1.SignInRequest) (*v1.SignInReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.pointUC.SignIn(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return &v1.SignInReply{
		SignInDate:     result.SignInDate.In(cstZone).Format("2006-01-02"),
		RewardPoints:   result.RewardPoints,
		ContinuousDays: result.ContinuousDays,
		PointBalance:   result.PointBalance,
		AlreadySigned:  result.AlreadySigned,
	}, nil
}

// ---------------------------- 消息中心 ----------------------------

// ListNotifications 消息列表。
func (s *PassengerAccountService) ListNotifications(ctx context.Context, req *v1.ListNotificationsRequest) (*v1.ListNotificationsReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.notificationUC.List(ctx, passengerID, req.GetPage(), req.GetPageSize(), req.IsRead)
	if err != nil {
		return nil, err
	}
	reply := &v1.ListNotificationsReply{
		Total:       result.Total,
		Page:        result.Page,
		PageSize:    result.PageSize,
		UnreadCount: result.UnreadCount,
		Items:       make([]*v1.Notification, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		notification := &v1.Notification{
			Id:               item.ID,
			NotificationType: item.NotificationType,
			Title:            item.Title,
			Content:          item.Content,
			IsRead:           item.IsRead,
			CreatedAt:        formatTime(item.CreatedAt),
		}
		// 关联订单可空（系统/活动消息无订单），NULL 时保持 0 值。
		if item.RelatedOrderID != nil {
			notification.RelatedOrderId = *item.RelatedOrderID
		}
		// 已读时间仅已读消息有值，未读保持空串。
		if item.ReadAt != nil {
			notification.ReadAt = formatTime(*item.ReadAt)
		}
		reply.Items = append(reply.Items, notification)
	}
	return reply, nil
}

// MarkNotificationsRead 消息已读标记。
func (s *PassengerAccountService) MarkNotificationsRead(ctx context.Context, req *v1.MarkNotificationsReadRequest) (*v1.MarkNotificationsReadReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	updated, unread, err := s.notificationUC.MarkRead(ctx, passengerID, req.GetIds(), req.GetMarkAll())
	if err != nil {
		return nil, err
	}
	return &v1.MarkNotificationsReadReply{UpdatedCount: updated, UnreadCount: unread}, nil
}

// ---------------------------- 实名认证 ----------------------------

// SubmitRealName 提交实名认证（姓名/证件号只以掩码与哈希入库）。
func (s *PassengerAccountService) SubmitRealName(ctx context.Context, req *v1.SubmitRealNameRequest) (*v1.SubmitRealNameReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	record, err := s.realNameUC.Submit(ctx, passengerID, req.GetRealName(), req.GetIdCardNo())
	if err != nil {
		return nil, err
	}
	return &v1.SubmitRealNameReply{
		RealNameStatus: int32(record.Status),
		RealNameMasked: record.RealNameMasked,
		IdCardMasked:   record.IDCardMasked,
		SubmittedAt:    formatTime(record.SubmittedAt),
	}, nil
}

// GetRealName 查询实名认证状态；从未提交时 record_status 为 0。
func (s *PassengerAccountService) GetRealName(ctx context.Context, _ *v1.GetRealNameRequest) (*v1.GetRealNameReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	info, err := s.realNameUC.Get(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	reply := &v1.GetRealNameReply{RealNameStatus: int32(info.PassengerStatus)}
	if info.Record != nil {
		reply.RecordStatus = int32(info.Record.Status)
		reply.RealNameMasked = info.Record.RealNameMasked
		reply.IdCardMasked = info.Record.IDCardMasked
		reply.SubmittedAt = formatTime(info.Record.SubmittedAt)
		reply.RejectReason = info.Record.RejectReason
		// 审核时间仅通过/驳回后才有值，未审核保持空串。
		if info.Record.VerifiedAt != nil {
			reply.VerifiedAt = formatTime(*info.Record.VerifiedAt)
		}
	}
	return reply, nil
}

// ---------------------------- 登录设备与登录记录 ----------------------------

// ListLoginDevices 登录设备列表（账号管理页「登录设备」）。
func (s *PassengerAccountService) ListLoginDevices(ctx context.Context, req *v1.ListLoginDevicesRequest) (*v1.ListLoginDevicesReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.loginDeviceUC.ListDevices(ctx, passengerID, req.GetPage(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	reply := &v1.ListLoginDevicesReply{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Items:    make([]*v1.LoginDevice, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		reply.Items = append(reply.Items, &v1.LoginDevice{
			DeviceId:          item.DeviceID,
			DeviceName:        item.DeviceName,
			DeviceModel:       item.DeviceModel,
			OsName:            item.OSName,
			OsVersion:         item.OSVersion,
			AppVersion:        item.AppVersion,
			LastLoginIp:       iputil.Mask(item.LastLoginIP),
			LastLoginLocation: item.LastLoginLocation,
			LoginCount:        item.LoginCount,
			FirstLoginAt:      formatTime(item.FirstLoginAt),
			LastLoginAt:       formatTime(item.LastLoginAt),
		})
	}
	return reply, nil
}

// ListLoginRecords 登录记录（成功与失败都返回）。
func (s *PassengerAccountService) ListLoginRecords(ctx context.Context, req *v1.ListLoginRecordsRequest) (*v1.ListLoginRecordsReply, error) {
	passengerID, err := s.currentPassengerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.loginDeviceUC.ListRecords(ctx, passengerID, req.GetPage(), req.GetPageSize(), req.LoginResult)
	if err != nil {
		return nil, err
	}
	reply := &v1.ListLoginRecordsReply{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Items:    make([]*v1.LoginRecord, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		reply.Items = append(reply.Items, &v1.LoginRecord{
			Id:            item.ID,
			LoginAt:       formatTime(item.LoginAt),
			DeviceId:      item.DeviceID,
			DeviceName:    item.DeviceName,
			DeviceModel:   item.DeviceModel,
			LoginIp:       iputil.Mask(item.LoginIP),
			LoginLocation: item.LoginLocation,
			AuthType:      int32(item.AuthType),
			LoginResult:   int32(item.LoginResult),
			FailureReason: item.FailureReason,
		})
	}
	return reply, nil
}

// ---------------------------- 内部工具 ----------------------------

// currentPassengerID 从 Authorization: Bearer <token> 解析乘客身份。
// 说明：模块6统一鉴权中间件落地后，本方法改为从统一鉴权上下文读取；
// 在此之前作为服务层兜底，保证本模块所有读写都按乘客隔离数据。
func (s *PassengerAccountService) currentPassengerID(ctx context.Context) (int64, error) {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return 0, biz.ErrUnauthorized
	}
	authorization := strings.TrimSpace(tr.RequestHeader().Get("Authorization"))
	if authorization == "" {
		return 0, biz.ErrUnauthorized
	}
	return s.authUC.Authenticate(ctx, authorization)
}

// clientVersion 客户端版本号（请求头 X-Client-Version，前端接口文档 §1.2）。
func clientVersion(ctx context.Context) string {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(tr.RequestHeader().Get("X-Client-Version"))
}

// clientMeta 提取客户端来源信息，用于登录审计（不记录敏感明文）。
//
// IP 顺序：X-Forwarded-For > X-Real-Ip > TCP 连接地址。
// 说明：本地 H5 经 Vite 代理访问时没有转发头，只有 RemoteAddr 可用，
// 不取 RemoteAddr 会导致「登录地址」永远为空。
func clientMeta(ctx context.Context) (ip, userAgent, requestID string) {
	if tr, ok := transport.FromServerContext(ctx); ok {
		header := tr.RequestHeader()
		userAgent = header.Get("User-Agent")
		requestID = header.Get("X-Request-Id")
		ip = iputil.Normalize(header.Get("X-Forwarded-For"))
		if ip == "" {
			ip = iputil.Normalize(header.Get("X-Real-Ip"))
		}
	}
	if ip == "" {
		if req, ok := khttp.RequestFromServerContext(ctx); ok && req != nil {
			ip = iputil.Normalize(req.RemoteAddr)
		}
	}
	return ip, userAgent, requestID
}

func toPassengerSummary(p *biz.Passenger) *v1.PassengerSummary {
	if p == nil {
		return nil
	}
	return &v1.PassengerSummary{
		Id:             p.ID,
		PassengerNo:    p.PassengerNo,
		Mobile:         mask.Mobile(p.Mobile),
		Nickname:       p.Nickname,
		AvatarUrl:      p.AvatarURL,
		RealNameStatus: int32(p.RealNameStatus),
		AccountStatus:  int32(p.AccountStatus),
		PointBalance:   p.PointBalance,
		Preferences:    toPreferences(&p.Preferences),
		CreatedAt:      formatTime(p.CreatedAt),
	}
}

func toPreferences(p *biz.PassengerPreferences) *v1.PassengerPreferences {
	if p == nil {
		return nil
	}
	return &v1.PassengerPreferences{
		OrderNotificationEnabled:     p.OrderNotificationEnabled,
		PromotionNotificationEnabled: p.PromotionNotificationEnabled,
		SecurityNotificationEnabled:  p.SecurityNotificationEnabled,
		TripShareDefaultEnabled:      p.TripShareDefaultEnabled,
	}
}

func toProtoAddress(a *biz.Address) *v1.PassengerAddress {
	if a == nil {
		return nil
	}
	address := &v1.PassengerAddress{
		Id:          a.ID,
		AddressType: a.AddressType,
		AddressName: a.AddressName,
		Address:     a.Address,
		Longitude:   a.Longitude,
		Latitude:    a.Latitude,
		StationId:   a.StationID,
		IsDefault:   a.IsDefault,
		CreatedAt:   formatTime(a.CreatedAt),
		UpdatedAt:   formatTime(a.UpdatedAt),
	}
	if a.LastUsedAt != nil {
		address.LastUsedAt = formatTime(*a.LastUsedAt)
	}
	return address
}

// formatTime 统一输出 ISO 8601（+08:00）。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(cstZone).Format(time.RFC3339)
}
