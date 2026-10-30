// Package device 从 User-Agent 解析登录设备信息（设备型号 / 设备名称 / 操作系统）。
//
// 设计约定（.agent/01 §3：pkg/ 放格式解析）：
//  1. 纯函数、无外部依赖、不读配置，便于单测与其他端复用；
//  2. 只做「尽力解析」：识别不出的部分返回空串或「未知设备」，绝不返回 error 阻断登录；
//  3. 解析结果只用于展示与审计，不参与鉴权判断。
package device

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// 长度上限与数据库列宽保持一致（passenger_login_devices）。
const (
	maxNameLen    = 80
	maxModelLen   = 60
	maxOSNameLen  = 30
	maxOSVerLen   = 30
	unknownDevice = "未知设备"
	nameSeparator = " · "
)

// Info 解析出的设备信息。
type Info struct {
	// Name 设备名称（展示用），如「iPhone · Safari」。
	Name string
	// Model 设备型号，如「iPhone 15」「SM-G9910」「PC」。
	Model string
	// OSName 操作系统，如「iOS」「Android」「Windows」。
	OSName string
	// OSVersion 系统版本，如「17.0」「13」。
	OSVersion string
}

var (
	// iosVersionPattern 匹配 iOS 版本：CPU iPhone OS 17_0 ...
	iosVersionPattern = regexp.MustCompile(`OS (\d+)[_.](\d+)`)
	// androidVersionPattern 匹配 Android 版本号。
	androidVersionPattern = regexp.MustCompile(`Android\s+([\d.]+)`)
	// androidModelPattern 匹配 Android 机型：Android 13; SM-G9910 Build/...
	androidModelPattern = regexp.MustCompile(`Android\s+[\d.]+;\s*([^;)]+?)(?:\s+Build/|\)|;)`)
	// windowsVersionPattern 匹配 Windows NT 版本。
	windowsVersionPattern = regexp.MustCompile(`Windows NT ([\d.]+)`)
	// macVersionPattern 匹配 macOS 版本：Mac OS X 10_15_7。
	macVersionPattern = regexp.MustCompile(`Mac OS X (\d+)[_.](\d+)`)
)

// Parse 解析 User-Agent，空串或无法识别时返回「未知设备」。
func Parse(userAgent string) Info {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return Info{Name: unknownDevice, Model: unknownDevice}
	}

	info := Info{}
	browser := detectBrowser(ua)
	lower := strings.ToLower(ua)

	switch {
	case strings.Contains(ua, "iPhone"):
		info.Model = "iPhone"
		info.OSName = "iOS"
		info.OSVersion = matchVersion(iosVersionPattern, ua)
	case strings.Contains(ua, "iPad"):
		info.Model = "iPad"
		info.OSName = "iPadOS"
		info.OSVersion = matchVersion(iosVersionPattern, ua)
	case strings.Contains(lower, "harmonyos") || strings.Contains(lower, "openharmony"):
		info.OSName = "HarmonyOS"
		info.Model = firstNonEmpty(matchAndroidModel(ua), "HarmonyOS 设备")
		info.OSVersion = matchVersion(androidVersionPattern, ua)
	case strings.Contains(ua, "Android"):
		info.OSName = "Android"
		info.OSVersion = matchVersion(androidVersionPattern, ua)
		info.Model = firstNonEmpty(matchAndroidModel(ua), "Android 设备")
	case strings.Contains(ua, "Windows"):
		info.OSName = "Windows"
		info.OSVersion = windowsVersionLabel(matchVersion(windowsVersionPattern, ua))
		info.Model = "PC"
	case strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Mac OS X"):
		info.OSName = "macOS"
		info.OSVersion = matchVersion(macVersionPattern, ua)
		info.Model = "Mac"
	case strings.Contains(ua, "Linux"):
		info.OSName = "Linux"
		info.Model = "PC"
	default:
		info.Model = unknownDevice
	}

	// 设备名称 = 型号 + 浏览器；型号未知时只展示浏览器，避免出现「未知设备 · 命令行」。
	model := strings.TrimSpace(info.Model)
	name := ""
	switch {
	case model != "" && model != unknownDevice && browser != "":
		name = model + nameSeparator + browser
	case model != "" && model != unknownDevice:
		name = model
	case browser != "":
		name = browser
	default:
		name = unknownDevice
	}
	info.Name = clip(name, maxNameLen)
	info.Model = clip(info.Model, maxModelLen)
	info.OSName = clip(info.OSName, maxOSNameLen)
	info.OSVersion = clip(info.OSVersion, maxOSVerLen)
	return info
}

// detectBrowser 识别常见浏览器/内置容器，识别不到返回空串。
func detectBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "MicroMessenger"):
		return "微信"
	case strings.Contains(ua, "AlipayClient"):
		return "支付宝"
	case strings.Contains(ua, "Edg"):
		return "Edge"
	case strings.Contains(ua, "Firefox"):
		return "Firefox"
	case strings.Contains(ua, "Chrome") || strings.Contains(ua, "CriOS"):
		return "Chrome"
	case strings.Contains(ua, "Safari"):
		return "Safari"
	case strings.Contains(ua, "curl"):
		return "命令行"
	default:
		return ""
	}
}

// matchVersion 取正则前两个分组拼成 x.y，失败返回空串。
func matchVersion(pattern *regexp.Regexp, ua string) string {
	matched := pattern.FindStringSubmatch(ua)
	if len(matched) < 2 {
		return ""
	}
	if len(matched) == 2 {
		return matched[1]
	}
	return matched[1] + "." + matched[2]
}

// matchAndroidModel 提取 Android 机型，识别不出返回空串。
func matchAndroidModel(ua string) string {
	matched := androidModelPattern.FindStringSubmatch(ua)
	if len(matched) < 2 {
		return ""
	}
	model := strings.TrimSpace(matched[1])
	// 「wv」是 WebView 标记，不是机型。
	if strings.EqualFold(model, "wv") {
		return ""
	}
	return model
}

// windowsVersionLabel 把 NT 版本号翻译成用户能看懂的 Windows 版本。
func windowsVersionLabel(ntVersion string) string {
	switch {
	case strings.HasPrefix(ntVersion, "10"), strings.HasPrefix(ntVersion, "11"):
		return "10/11"
	case ntVersion == "6.3":
		return "8.1"
	case ntVersion == "6.2":
		return "8"
	case ntVersion == "6.1":
		return "7"
	default:
		return ntVersion
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// clip 按字符数截断，避免中文按字节截出乱码。
func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit])
}
