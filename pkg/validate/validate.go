// Package validate 收敛格式校验与规范化工具（.agent/01 §3：pkg/ 放格式校验）。
//
// 设计约定：
//  1. 本包只做「格式」判断与规范化，**不带业务语义**，因此不依赖 kratos error、不产出业务错误码；
//  2. 返回错误统一为 *Error，只携带面向用户的提示文案；业务层用 invalidArgument(err.Error())
//     包装成带业务码的错误（AGENT.MD §5：业务错误必须使用已有错误码枚举）；
//  3. 敏感字段（手机号、姓名、证件号）只在这里校验，不在各处重复实现。
package validate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 校验相关的长度约束集中声明，避免魔法数字散落。
const (
	// MobileLength 大陆手机号固定 11 位。
	MobileLength = 11
	// RealNameMinLen 姓名最短字符数。
	RealNameMinLen = 2
	// RealNameMaxLen 姓名最长字符数。
	RealNameMaxLen = 30
	// NicknameMinLen 昵称最短字符数。
	NicknameMinLen = 2
	// NicknameMaxLen 昵称最长字符数（与 passengers.nickname varchar(60) 兼容）。
	NicknameMaxLen = 20
	// IDCardLength 二代身份证固定 18 位。
	IDCardLength = 18
)

// mobilePattern 大陆手机号：1 开头，第二位 3-9，共 11 位。
var mobilePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// idCardWeights 身份证前 17 位的加权因子（GB 11643-1999）。
var idCardWeights = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}

// idCardCheckCodes 加权和余数（0-10）到校验位的映射表（GB 11643-1999）。
var idCardCheckCodes = [11]byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}

// Error 校验错误：只承载提示文案，业务码由调用层补齐。
type Error struct {
	// Message 面向用户的提示，可直接透出到接口 message 字段。
	Message string
}

// Error 实现 error 接口。
func (e *Error) Error() string { return e.Message }

// newError 构造校验错误。
func newError(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// Mobile 规范化并校验手机号：去首尾空格、去 +86 前缀、去空格与短横线。
// 合法返回纯 11 位数字，非法返回 *Error。
func Mobile(raw string) (string, error) {
	// 先做无损清洗：用户从通讯录复制时常带 +86、空格、短横线。
	m := strings.TrimSpace(raw)
	m = strings.TrimPrefix(m, "+86")
	m = strings.ReplaceAll(m, " ", "")
	m = strings.ReplaceAll(m, "-", "")
	// 空值与格式分别给不同提示，便于前端定位。
	if m == "" {
		return "", newError("手机号不能为空")
	}
	if !mobilePattern.MatchString(m) {
		return "", newError("手机号格式不正确")
	}
	return m, nil
}

// Nickname 规范化并校验昵称：2-20 个字符，允许中英文、数字、下划线、短横线、空格与间隔号。
// 禁止换行/控制字符，避免污染列表展示与导出文件。
func Nickname(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", newError("用户名不能为空")
	}
	if count := utf8.RuneCountInString(name); count < NicknameMinLen || count > NicknameMaxLen {
		return "", newError("用户名长度需为 %d-%d 个字符", NicknameMinLen, NicknameMaxLen)
	}
	for _, r := range name {
		if r == ' ' || r == '_' || r == '-' || r == '·' || r == '•' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return "", newError("用户名只能包含中英文、数字、下划线、短横线或空格")
	}
	return name, nil
}

// RealName 规范化并校验姓名：2-30 个字符，允许中文、英文字母、空格与间隔号。
func RealName(raw string) (string, error) {
	// 姓名首尾空白不参与长度计算。
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", newError("真实姓名不能为空")
	}
	// 按 Unicode 字符数（rune）计数，避免中文按字节数误判。
	if count := utf8.RuneCountInString(name); count < RealNameMinLen || count > RealNameMaxLen {
		return "", newError("真实姓名长度需为 %d-%d 个字符", RealNameMinLen, RealNameMaxLen)
	}
	// 逐字符判断：空格与间隔号放行，其余必须是中文或字母。
	for _, r := range name {
		if r == ' ' || r == '·' || r == '•' {
			continue
		}
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) {
			continue
		}
		return "", newError("真实姓名只能包含中文、字母、空格或间隔号")
	}
	return name, nil
}

// IDCard 规范化并校验身份证号：统一大写、去空格，校验长度、出生日期与校验位。
func IDCard(raw string) (string, error) {
	// 末位 x 统一转大写，避免同一证件因大小写被判成两条。
	idCard := strings.ToUpper(strings.TrimSpace(raw))
	idCard = strings.ReplaceAll(idCard, " ", "")
	if idCard == "" {
		return "", newError("身份证号不能为空")
	}
	// 长度校验
	if len(idCard) != IDCardLength {
		return "", newError("身份证号需为 %d 位", IDCardLength)
	}
	// 前 17 位必须为数字
	for i := 0; i < IDCardLength-1; i++ {
		if idCard[i] < '0' || idCard[i] > '9' {
			return "", newError("身份证号前 17 位必须为数字")
		}
	}
	// 校验位允许数字或 X
	last := idCard[IDCardLength-1]
	if (last < '0' || last > '9') && last != 'X' {
		return "", newError("身份证号校验位不合法")
	}
	// 第 7-14 位是出生日期，必须可解析且在合理区间（1900 年至今）。
	birth, err := time.Parse("20060102", idCard[6:14])
	if err != nil || birth.Year() < 1900 || birth.After(time.Now()) {
		return "", newError("身份证号中的出生日期不合法")
	}
	// 加权求和取模比对校验位，防止手填假号。
	sum := 0
	for i := 0; i < IDCardLength-1; i++ {
		sum += int(idCard[i]-'0') * idCardWeights[i]
	}
	if expected := idCardCheckCodes[sum%11]; expected != last {
		return "", newError("身份证号校验位不正确")
	}
	return idCard, nil
}
