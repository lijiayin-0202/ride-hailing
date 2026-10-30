// 模块1 用户账号与个人中心（成员A）
// 访问令牌实现（JWT HS256）。
//
// 说明：按《萝卜快跑前端页面接口文档》§7.1「令牌结构待认证系统确定」，
// 本模块先给出可用的令牌实现；模块6统一认证设施（含刷新令牌、可撤销会话）落地后
// 只需替换 biz.TokenIssuer 的实现，业务代码无需改动。
package data

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/golang-jwt/jwt/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

const tokenIssuerName = "ride-hailing"

type tokenIssuer struct {
	// secret HS256 签名密钥，仅内存持有，禁止落盘/落日志。
	secret []byte
	// ttl 令牌有效期（当前 2 小时，见 biz.AccessTokenTTL）。
	ttl time.Duration
	// log 结构化日志。
	log *log.Helper
}

// NewTokenIssuer 令牌签发器。
// 密钥来源：环境变量 RIDE_JWT_SECRET（禁止硬编码，AGENT.md §10.1）。
// 未配置时使用进程内随机密钥并告警，保证开发环境可启动，重启后旧令牌失效。
func NewTokenIssuer(logger log.Logger) biz.TokenIssuer {
	helper := log.NewHelper(log.With(logger, "module", "data/token"))

	// 密钥读取收敛到 internal/conf（.agent/04 §2.1/§3）。
	secret := conf.JWTSecret()
	var key []byte
	if secret == "" {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			key = []byte(fmt.Sprintf("insecure-%d", time.Now().UnixNano()))
		}
		helper.Warn("未配置 RIDE_JWT_SECRET，已生成进程内随机密钥：服务重启后旧令牌将失效")
	} else {
		key = []byte(secret)
	}

	return &tokenIssuer{secret: key, ttl: biz.AccessTokenTTL, log: helper}
}

// Issue 签发访问令牌，sub 为乘客ID，附加乘客编号便于排障。
func (t *tokenIssuer) Issue(ctx context.Context, p *biz.Passenger) (string, int32, error) {
	if p == nil || p.ID <= 0 {
		return "", 0, biz.ErrUnauthorized
	}
	// 标准声明：iss 固定服务名，exp 必填；jti 便于后续做单点撤销。
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": tokenIssuerName,
		"sub": strconv.FormatInt(p.ID, 10),
		"pno": p.PassengerNo,
		"iat": now.Unix(),
		"exp": now.Add(t.ttl).Unix(),
		"jti": randomTokenID(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", 0, err
	}
	return token, int32(t.ttl / time.Second), nil
}

// Parse 校验令牌并返回乘客ID。显式限定签名算法，防止算法混淆攻击。
func (t *tokenIssuer) Parse(ctx context.Context, token string) (int64, error) {
	parsed, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		// 限定 HMAC 族算法，防止 alg=none / 算法混淆攻击。
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithIssuer(tokenIssuerName), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return 0, biz.ErrUnauthorized
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return 0, biz.ErrUnauthorized
	}
	// sub 存乘客 ID，必须能解析成正整数，否则视为非法令牌。
	sub, err := claims.GetSubject()
	if err != nil {
		return 0, biz.ErrUnauthorized
	}
	passengerID, err := strconv.ParseInt(sub, 10, 64)
	if err != nil || passengerID <= 0 {
		return 0, biz.ErrUnauthorized
	}
	return passengerID, nil
}

// randomTokenID 生成 jti：8 字节随机数的十六进制；随机源不可用时退化为时间戳（仍可唯一）。
func randomTokenID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf)
}

var _ biz.TokenIssuer = (*tokenIssuer)(nil)
