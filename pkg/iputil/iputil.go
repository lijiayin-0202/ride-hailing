// Package iputil 收敛客户端 IP 的规范化、脱敏与归属地兜底展示。
//
// 设计约定：
//  1. 本项目未接入第三方 IP 归属地库，这里只区分「本机 / 内网 / 未知归属地」；
//     接入真实 IP 库时替换 ResolveLocation 实现即可，调用方无需改动；
//  2. 接口出参一律使用 Mask（最小返回原则），审计表可保存原始 IP 用于风控排查。
package iputil

import (
	"net"
	"strconv"
	"strings"
)

// 未知 IP 的统一展示文案。
const (
	LocationUnknown  = "未知归属地"
	LocationLocal    = "本机"
	LocationIntranet = "内网地址"
	LocationEmpty    = "未知"
)

// Normalize 从 X-Forwarded-For 等多值头里取出首个合法 IP。
// 形如 "114.114.114.114, 10.0.0.1" 时返回 "114.114.114.114"。
func Normalize(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if idx := strings.Index(value, ","); idx > 0 {
		value = strings.TrimSpace(value[:idx])
	}
	// 去掉端口（IPv4:port 形式）
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.TrimSpace(value)
}

// Mask 脱敏：IPv4 保留前两段（114.114.*.*），IPv6 保留前两组（240e:3b4:*:*）。
// 非法输入返回空串，避免脏数据透出。
func Mask(ip string) string {
	parsed := net.ParseIP(Normalize(ip))
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return strings.Join([]string{
			strconv.Itoa(int(v4[0])), strconv.Itoa(int(v4[1])), "*", "*",
		}, ".")
	}
	// IPv6：取前两组十六进制
	text := parsed.String()
	groups := strings.Split(text, ":")
	if len(groups) >= 2 {
		return groups[0] + ":" + groups[1] + ":*:*"
	}
	return "*"
}

// ResolveLocation 归属地兜底解析。
// 当前实现不做外部查询：回环地址返回「本机」，私网地址返回「内网地址」，
// 其余公网地址在未接入 IP 库前统一返回「未知归属地」。
func ResolveLocation(ip string) string {
	normalized := Normalize(ip)
	if normalized == "" {
		return LocationEmpty
	}
	parsed := net.ParseIP(normalized)
	if parsed == nil {
		return LocationUnknown
	}
	if parsed.IsLoopback() {
		return LocationLocal
	}
	if parsed.IsPrivate() || parsed.IsLinkLocalUnicast() {
		return LocationIntranet
	}
	return LocationUnknown
}
