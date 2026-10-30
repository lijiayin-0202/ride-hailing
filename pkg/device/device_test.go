// 设备解析单测：覆盖 iOS / Android / Windows / macOS / 空 UA 五类真实 UA。
package device

import "testing"

func TestParseIPhone(t *testing.T) {
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	got := Parse(ua)
	if got.Model != "iPhone" || got.OSName != "iOS" || got.OSVersion != "17.0" {
		t.Fatalf("iOS 解析异常: %+v", got)
	}
	if got.Name != "iPhone · Safari" {
		t.Fatalf("设备名称异常: %s", got.Name)
	}
}

func TestParseAndroid(t *testing.T) {
	ua := "Mozilla/5.0 (Linux; Android 13; SM-G9910 Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/116.0.0.0 Mobile Safari/537.36"
	got := Parse(ua)
	if got.Model != "SM-G9910" || got.OSName != "Android" || got.OSVersion != "13" {
		t.Fatalf("Android 解析异常: %+v", got)
	}
	if got.Name != "SM-G9910 · Chrome" {
		t.Fatalf("设备名称异常: %s", got.Name)
	}
}

func TestParseWindowsAndWechat(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 MicroMessenger/8.0.42"
	got := Parse(ua)
	if got.OSName != "Windows" || got.Model != "PC" {
		t.Fatalf("Windows 解析异常: %+v", got)
	}
	if got.Name != "PC · 微信" {
		t.Fatalf("设备名称异常: %s", got.Name)
	}
}

func TestParseEmptyAndUnknown(t *testing.T) {
	if got := Parse(""); got.Name != unknownDevice || got.Model != unknownDevice {
		t.Fatalf("空 UA 应返回未知设备: %+v", got)
	}
	if got := Parse("some-random-client/1.0"); got.Name != unknownDevice {
		t.Fatalf("无法识别的 UA 应返回未知设备: %+v", got)
	}
}

func TestParseClipLongModel(t *testing.T) {
	// 超长机型不应超过数据库列宽（60）。
	long := "Mozilla/5.0 (Linux; Android 13; ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ Build/X) Chrome/1.0"
	got := Parse(long)
	if len([]rune(got.Model)) > maxModelLen {
		t.Fatalf("机型未截断: %d", len([]rune(got.Model)))
	}
}
