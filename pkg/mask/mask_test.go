// pkg/mask 单元测试：手机号、姓名、身份证脱敏结果与异常长度兜底。
package mask

import "testing"

func TestMobile(t *testing.T) {
	if got := Mobile("13800000000"); got != "138****0000" {
		t.Fatalf("Mobile 脱敏结果不符: %s", got)
	}
	// 非 11 位一律返回固定掩码，避免异常数据泄露。
	if got := Mobile("1380000"); got != "***" {
		t.Fatalf("异常长度应返回 ***，实际 %s", got)
	}
}

func TestRealName(t *testing.T) {
	cases := map[string]string{
		"张三":   "张*",
		"张三丰":  "张**",
		"欧阳娜娜": "欧***",
		"":     "",
	}
	for in, want := range cases {
		if got := RealName(in); got != want {
			t.Fatalf("RealName(%q) = %q；期望 %q", in, got, want)
		}
	}
}

func TestIDCard(t *testing.T) {
	if got := IDCard("11010519491231002X"); got != "110105********002X" {
		t.Fatalf("IDCard 脱敏结果不符: %s", got)
	}
	// 长度不符返回固定掩码。
	if got := IDCard("11010519491231002"); got != "***" {
		t.Fatalf("异常长度应返回 ***，实际 %s", got)
	}
}
