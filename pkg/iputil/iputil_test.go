// IP 工具单测：覆盖多值头、端口、IPv4/IPv6 脱敏与归属地兜底。
package iputil

import "testing"

func TestNormalize(t *testing.T) {
	if got := Normalize(" 114.114.114.114 , 10.0.0.1 "); got != "114.114.114.114" {
		t.Fatalf("多值头解析异常: %s", got)
	}
	if got := Normalize("127.0.0.1:51234"); got != "127.0.0.1" {
		t.Fatalf("端口未剥离: %s", got)
	}
	if got := Normalize(""); got != "" {
		t.Fatalf("空值应为空串: %s", got)
	}
}

func TestMask(t *testing.T) {
	if got := Mask("114.114.114.114"); got != "114.114.*.*" {
		t.Fatalf("IPv4 脱敏异常: %s", got)
	}
	if got := Mask("not-an-ip"); got != "" {
		t.Fatalf("非法 IP 应返回空串: %s", got)
	}
	if got := Mask("240e:3b4:1234::1"); got != "240e:3b4:*:*" {
		t.Fatalf("IPv6 脱敏异常: %s", got)
	}
}

func TestResolveLocation(t *testing.T) {
	cases := map[string]string{
		"":                LocationEmpty,
		"127.0.0.1":       LocationLocal,
		"192.168.1.10":    LocationIntranet,
		"10.20.30.40":     LocationIntranet,
		"114.114.114.114": LocationUnknown,
	}
	for ip, want := range cases {
		if got := ResolveLocation(ip); got != want {
			t.Fatalf("IP %s 归属地应为 %s，实际 %s", ip, want, got)
		}
	}
}
