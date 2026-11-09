// pkg/validate 单元测试：手机号规范化、姓名与身份证格式/校验位。
// 用例中的身份证号为 GB 11643 示例号，非真实个人证件（AGENT.MD §9.4）。
package validate

import (
	"strings"
	"testing"
)

func TestMobileValid(t *testing.T) {
	cases := map[string]string{
		"13800000000":         "13800000000",
		" +86 138-0000-0000 ": "13800000000",
		"+8613800000000":      "13800000000",
		"19912345678":         "19912345678",
	}
	for in, want := range cases {
		got, err := Mobile(in)
		if err != nil || got != want {
			t.Fatalf("Mobile(%q) = %q, %v；期望 %q", in, got, err, want)
		}
	}
}

func TestMobileInvalid(t *testing.T) {
	cases := map[string]string{
		"":            "手机号不能为空",
		"12345":       "手机号格式不正确",
		"1280000000":  "手机号格式不正确",
		"1380000000a": "手机号格式不正确",
	}
	for in, wantMsg := range cases {
		_, err := Mobile(in)
		if err == nil {
			t.Fatalf("Mobile(%q) 应报错", in)
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Fatalf("Mobile(%q) 提示不符: %v", in, err)
		}
	}
}

func TestRealNameValid(t *testing.T) {
	cases := map[string]string{
		"张三":         "张三",
		"  李四  ":     "李四",
		"欧阳·娜娜":      "欧阳·娜娜",
		"John Smith": "John Smith",
	}
	for in, want := range cases {
		got, err := RealName(in)
		if err != nil || got != want {
			t.Fatalf("RealName(%q) = %q, %v；期望 %q", in, got, err, want)
		}
	}
}

func TestRealNameInvalid(t *testing.T) {
	cases := map[string]string{
		"":                      "真实姓名不能为空",
		"张":                     "真实姓名长度需为",
		strings.Repeat("张", 31): "真实姓名长度需为",
		"张三1":                   "真实姓名只能包含",
		"张三@":                   "真实姓名只能包含",
	}
	for in, wantMsg := range cases {
		_, err := RealName(in)
		if err == nil {
			t.Fatalf("RealName(%q) 应报错", in)
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Fatalf("RealName(%q) 提示不符: %v", in, err)
		}
	}
}

func TestIDCardValid(t *testing.T) {
	// 末位小写 x 应统一转大写。
	got, err := IDCard(" 11010519491231002x ")
	if err != nil || got != "11010519491231002X" {
		t.Fatalf("IDCard 规范化失败: %q %v", got, err)
	}
}

func TestIDCardInvalid(t *testing.T) {
	cases := map[string]string{
		"":                   "身份证号不能为空",
		"11010519491231002":  "身份证号需为",
		"1101051949123100AX": "身份证号前 17 位必须为数字",
		"11010519491231002Y": "身份证号校验位不合法",
		"11010519491331002X": "身份证号中的出生日期不合法",
		"110105194912310021": "身份证号校验位不正确",
	}
	for in, wantMsg := range cases {
		_, err := IDCard(in)
		if err == nil {
			t.Fatalf("IDCard(%q) 应报错", in)
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Fatalf("IDCard(%q) 提示不符: %v", in, err)
		}
	}
}
