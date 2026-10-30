// 模块1 用户账号与个人中心（成员A）—— 实名认证用例单元测试
// 覆盖：证件校验位、姓名/证件掩码、重复提交、证件重绑冲突、未提交查询。
// 全部使用内存假实现，不依赖数据库；测试用证件号为 GB 11643 示例号，非真实个人证件。
package biz

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	testIDCardValid   = "11010519491231002X"
	testIDCardBadSum  = "110105194912310021"
	testRealNameAlice = "张三"
)

type fakeRealNameRepo struct {
	record  *RealNameRecord
	boundID map[string]int64
}

func newFakeRealNameRepo() *fakeRealNameRepo {
	return &fakeRealNameRepo{boundID: map[string]int64{}}
}

func (f *fakeRealNameRepo) GetByPassenger(_ context.Context, passengerID int64) (*RealNameRecord, error) {
	if f.record == nil || f.record.PassengerID != passengerID {
		return nil, nil
	}
	copied := *f.record
	return &copied, nil
}

func (f *fakeRealNameRepo) Submit(_ context.Context, record *RealNameRecord) (*RealNameRecord, error) {
	record.ID = 1
	if f.record != nil {
		record.ID = f.record.ID
	}
	copied := *record
	f.record = &copied
	return record, nil
}

func (f *fakeRealNameRepo) IDCardBound(_ context.Context, idCardHash string, excludePassengerID int64) (bool, error) {
	owner, ok := f.boundID[idCardHash]
	return ok && owner != excludePassengerID, nil
}

func newRealNameTestFixture(passengerStatus int16) (*RealNameUsecase, *fakeRealNameRepo, *fakePassengerRepo, int64) {
	passengers := newFakePassengerRepo()
	passenger := &Passenger{ID: 101, RealNameStatus: passengerStatus, AccountStatus: AccountStatusNormal}
	passengers.byID[passenger.ID] = passenger
	passengers.byMobile[passenger.Mobile] = passenger

	repo := newFakeRealNameRepo()
	// 盐值缺失属于启动期错误（fail-closed），测试固定注入测试盐。
	uc, err := NewRealNameUsecase(repo, passengers, RealNameSalt("test-salt"), log.DefaultLogger)
	if err != nil {
		panic(err)
	}
	return uc, repo, passengers, passenger.ID
}

func TestSubmitRealNameSuccess(t *testing.T) {
	uc, repo, _, passengerID := newRealNameTestFixture(RealNameStatusNone)

	record, err := uc.Submit(context.Background(), passengerID, " "+testRealNameAlice+" ", testIDCardValid)
	if err != nil {
		t.Fatalf("提交实名认证失败: %v", err)
	}
	if record.Status != RealNameStatusPending {
		t.Fatalf("提交后状态应为认证中(1)，实际 %d", record.Status)
	}
	if record.RealNameMasked != "张*" {
		t.Fatalf("姓名掩码错误，实际 %q", record.RealNameMasked)
	}
	if record.IDCardMasked != "110105********002X" {
		t.Fatalf("证件掩码错误，实际 %q", record.IDCardMasked)
	}
	if record.IDCardHash == "" || strings.Contains(record.IDCardHash, testIDCardValid) {
		t.Fatalf("证件哈希不应为空且不得包含明文: %q", record.IDCardHash)
	}
	if repo.record == nil || repo.record.Status != RealNameStatusPending {
		t.Fatalf("仓储未写入认证记录")
	}
}

func TestSubmitRealNameRejectsBadIDCard(t *testing.T) {
	uc, _, _, passengerID := newRealNameTestFixture(RealNameStatusNone)

	cases := map[string]string{
		"校验位错误":  testIDCardBadSum,
		"长度不足":   "11010519491231002",
		"含非法字符":  "11010519491231002A",
		"出生日期非法": "11010519491331002X",
	}
	for name, idCard := range cases {
		if _, err := uc.Submit(context.Background(), passengerID, testRealNameAlice, idCard); err == nil {
			t.Fatalf("%s：应返回参数错误，实际通过", name)
		}
	}
}

func TestSubmitRealNameRejectsBadName(t *testing.T) {
	uc, _, _, passengerID := newRealNameTestFixture(RealNameStatusNone)

	for _, name := range []string{"", "张", "张三1", "张三@"} {
		if _, err := uc.Submit(context.Background(), passengerID, name, testIDCardValid); err == nil {
			t.Fatalf("姓名 %q 应被拒绝", name)
		}
	}
}

func TestSubmitRealNameRejectsVerifiedPassenger(t *testing.T) {
	uc, _, _, passengerID := newRealNameTestFixture(RealNameStatusVerified)

	_, err := uc.Submit(context.Background(), passengerID, testRealNameAlice, testIDCardValid)
	if err == nil {
		t.Fatal("已认证乘客不应允许重复提交")
	}
	if !strings.Contains(err.Error(), "已完成实名认证") {
		t.Fatalf("错误提示不符，实际 %v", err)
	}
}

func TestSubmitRealNameRejectsBoundIDCard(t *testing.T) {
	uc, repo, _, passengerID := newRealNameTestFixture(RealNameStatusNone)
	// 该证件已被乘客 999 绑定。
	repo.boundID[hashIDCard(testIDCardValid, "test-salt")] = 999

	_, err := uc.Submit(context.Background(), passengerID, testRealNameAlice, testIDCardValid)
	if err == nil {
		t.Fatal("已绑定其他账号的证件应被拒绝")
	}
	// 对外必须是模糊话术：不回显「已绑定其他账号」，避免被用来枚举证件是否注册过。
	if !strings.Contains(err.Error(), "证件信息校验未通过") {
		t.Fatalf("错误提示不符，实际 %v", err)
	}
	if strings.Contains(err.Error(), "已绑定") {
		t.Fatalf("错误提示泄露了证件占用信息：%v", err)
	}
}

func TestSubmitRealNameAllowsResubmitWhenRejected(t *testing.T) {
	uc, repo, _, passengerID := newRealNameTestFixture(RealNameStatusRejected)
	repo.record = &RealNameRecord{
		ID:           7,
		PassengerID:  passengerID,
		Status:       RealNameStatusRejected,
		RejectReason: "证件照片模糊",
	}

	record, err := uc.Submit(context.Background(), passengerID, testRealNameAlice, testIDCardValid)
	if err != nil {
		t.Fatalf("驳回后重新提交应允许: %v", err)
	}
	if record.ID != 7 {
		t.Fatalf("重新提交应覆盖原记录(ID=7)，实际 %d", record.ID)
	}
	if record.Status != RealNameStatusPending {
		t.Fatalf("重新提交后状态应为认证中，实际 %d", record.Status)
	}
}

func TestGetRealNameWhenNotSubmitted(t *testing.T) {
	uc, _, _, passengerID := newRealNameTestFixture(RealNameStatusNone)

	info, err := uc.Get(context.Background(), passengerID)
	if err != nil {
		t.Fatalf("查询实名状态失败: %v", err)
	}
	if info.PassengerStatus != RealNameStatusNone || info.Record != nil {
		t.Fatalf("未提交时状态应为 0 且无记录，实际 status=%d record=%v", info.PassengerStatus, info.Record)
	}
}
