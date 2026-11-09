// Package mask 收敛敏感信息脱敏工具（.agent/01 §3：pkg/ 放脱敏；AGENT.MD §10.2：日志禁止打印明文）。
//
// 使用约定：
//  1. 接口出参与日志打印前必须经过本包脱敏，禁止直接输出手机号、姓名、证件号；
//  2. 脱敏结果只用于展示，不能反解，不能参与业务判重（判重一律用哈希）。
package mask

import "strings"

// 脱敏固定掩码字符，保证长度不可推断出原文长度之外的信息。
const star = "*"

// Mobile 手机号脱敏：保留前 3 位与后 4 位（13800000000 → 138****0000）。
// 非 11 位输入一律返回 "***"，避免异常数据漏出。
func Mobile(mobile string) string {
	if len(mobile) != 11 {
		return "***"
	}
	return mobile[:3] + "****" + mobile[7:]
}

// RealName 姓名脱敏：保留首字，其余用 * 代替（张三 → 张*，张三丰 → 张**）。
func RealName(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[0]) + strings.Repeat(star, len(runes)-1)
}

// IDCard 身份证脱敏：保留前 6 位与后 4 位（11010519491231002X → 110105********002X）。
// 长度不符时返回 "***"，防止脏数据透出。
func IDCard(idCard string) string {
	if len(idCard) != 18 {
		return "***"
	}
	return idCard[:6] + strings.Repeat(star, 8) + idCard[14:]
}
