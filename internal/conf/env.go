// 配置读取唯一入口（.agent/04-deploy-config.md §2.1）
//
// 规则：
//  1. yaml 加载、环境变量读取、默认值填充全部收敛在 internal/conf；
//  2. api/ service/ biz/ data/ server/ cmd/ 禁止裸调用 os.Getenv / os.LookupEnv；
//  3. 敏感项（数据库 DSN、密钥、盐值）只允许从环境变量注入，禁止写入 yaml。
//
// 本文件不修改 conf.proto（避免动生成物）：新增配置项走「conf 暴露取值方法」，
// 待 Bootstrap 结构正式扩充时再迁移为 proto 字段。
package conf

import (
	"os"
	"strings"
)

// 环境变量名集中登记，避免业务代码散落字符串字面量。
const (
	EnvDatabaseSource = "RIDE_DATABASE_SOURCE"
	EnvJWTSecret      = "RIDE_JWT_SECRET"
	EnvSmsEchoCode    = "RIDE_SMS_ECHO_CODE"
	EnvIDCardSalt     = "RIDE_ID_CARD_SALT"
)

// DatabaseSource 数据库连接串（敏感项）。空值表示未注入，由调用方决定回退策略。
func DatabaseSource() string { return strings.TrimSpace(os.Getenv(EnvDatabaseSource)) }

// ExpandEnvPlaceholder 解析 yaml 中的 ${VAR} 占位符。
// 目的：让 yaml 只保留占位、不落敏感明文（.agent/04 §1）；非占位形式原样返回。
// 未注入变量时返回空串，由调用方给出明确报错，避免把 ${VAR} 当成 DSN 去解析。
func ExpandEnvPlaceholder(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "${") || !strings.HasSuffix(raw, "}") {
		return raw
	}
	return strings.TrimSpace(os.Getenv(strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}")))
}

// JWTSecret 令牌签名密钥（敏感项）。空值表示未配置，由实现方生成进程内随机密钥。
func JWTSecret() string { return strings.TrimSpace(os.Getenv(EnvJWTSecret)) }

// SmsEchoCodeEnabled 联调开关：验证码写入服务日志，生产必须关闭。
func SmsEchoCodeEnabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvSmsEchoCode))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true")
}

// IDCardSalt 身份证哈希盐值（敏感项）。空值与未配置等价，由调用方使用兜底策略并告警。
func IDCardSalt() string { return strings.TrimSpace(os.Getenv(EnvIDCardSalt)) }
