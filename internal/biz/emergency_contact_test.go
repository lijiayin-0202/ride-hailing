// 模块1 用户账号与个人中心（成员A）—— 紧急联系人用例单元测试
// 覆盖：姓名长度/字符校验、手机号规范化、关系长度、未维护查询返回空。
// 全部使用内存假实现，不依赖数据库与 Redis。
package biz

import (
	"context"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

// fakeEmergencyContactRepo 内存假实现：一人一条，重复保存覆盖。
type fakeEmergencyContactRepo struct {
	saved *EmergencyContact
}

func (f *fakeEmergencyContactRepo) Get(_ context.Context, passengerID int64) (*EmergencyContact, error) {
	if f.saved == nil || f.saved.PassengerID != passengerID {
		return nil, nil
	}
	copied := *f.saved
	return &copied, nil
}

func (f *fakeEmergencyContactRepo) Save(_ context.Context, contact *EmergencyContact) error {
	contact.ID = 1
	copied := *contact
	f.saved = &copied
	return nil
}

func newEmergencyContactUsecase(repo EmergencyContactRepo) *EmergencyContactUsecase {
	return NewEmergencyContactUsecase(repo, log.DefaultLogger)
}

// TestEmergencyContactSave_Valid 校验合法入参能保存并完成手机号规范化。
func TestEmergencyContactSave_Valid(t *testing.T) {
	repo := &fakeEmergencyContactRepo{}
	uc := newEmergencyContactUsecase(repo)

	contact, err := uc.Save(context.Background(), 1001, " 李四 ", "+86 138-0000-0000", " 配偶 ")
	if err != nil {
		t.Fatalf("合法入参应保存成功，实际: %v", err)
	}
	if contact.ContactName != "李四" {
		t.Fatalf("姓名应去除首尾空格，实际: %q", contact.ContactName)
	}
	// +86、空格与短横线应被清洗成纯 11 位号码。
	if contact.ContactMobile != "13800000000" {
		t.Fatalf("手机号应规范化，实际: %q", contact.ContactMobile)
	}
	if contact.Relation != "配偶" {
		t.Fatalf("关系应去除首尾空格，实际: %q", contact.Relation)
	}
	if repo.saved == nil || repo.saved.PassengerID != 1001 {
		t.Fatalf("应写入归属乘客，实际: %+v", repo.saved)
	}
}

// TestEmergencyContactSave_InvalidName 姓名过短或含非法字符应拒绝。
func TestEmergencyContactSave_InvalidName(t *testing.T) {
	uc := newEmergencyContactUsecase(&fakeEmergencyContactRepo{})
	cases := []string{"李", "李四123"}
	for _, name := range cases {
		if _, err := uc.Save(context.Background(), 1001, name, "13800000000", ""); err == nil {
			t.Fatalf("姓名 %q 应校验失败", name)
		}
	}
}

// TestEmergencyContactSave_InvalidMobile 手机号非法应拒绝。
func TestEmergencyContactSave_InvalidMobile(t *testing.T) {
	uc := newEmergencyContactUsecase(&fakeEmergencyContactRepo{})
	if _, err := uc.Save(context.Background(), 1001, "李四", "12345", ""); err == nil {
		t.Fatal("非法手机号应校验失败")
	}
}

// TestEmergencyContactSave_RelationTooLong 关系超过 20 字符应拒绝。
func TestEmergencyContactSave_RelationTooLong(t *testing.T) {
	uc := newEmergencyContactUsecase(&fakeEmergencyContactRepo{})
	if _, err := uc.Save(context.Background(), 1001, "李四", "13800000000", strings.Repeat("朋", 21)); err == nil {
		t.Fatal("关系超长应校验失败")
	}
}

// TestEmergencyContactGet_NotMaintained 未维护时返回空，不报错。
func TestEmergencyContactGet_NotMaintained(t *testing.T) {
	uc := newEmergencyContactUsecase(&fakeEmergencyContactRepo{})
	contact, err := uc.Get(context.Background(), 1001)
	if err != nil {
		t.Fatalf("未维护不应报错，实际: %v", err)
	}
	if contact != nil {
		t.Fatalf("未维护应返回空，实际: %+v", contact)
	}
}
