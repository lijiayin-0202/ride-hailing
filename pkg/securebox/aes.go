// Package securebox 提供敏感字段的对称加密（AES-256-GCM）。
//
// 使用场景（UA-T0-09）：姓名、身份证号除掩码与哈希外，另存一份可解密密文，
// 供核验/合规调取时在服务端解密；接口层只返回掩码，绝不返回解密结果。
//
// 密文格式：`v1:<base64(nonce|密文|tag)>`
//   - 前缀 `v1` 为密钥版本号，后续轮换密钥时新增 v2，解密按前缀选择对应密钥；
//   - 每次加密使用新的随机 nonce（GCM 要求 nonce 不重复）；
//   - 空串原样返回，表示"未填写"，避免把空值加密成无意义密文。
package securebox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// KeySize AES-256 密钥字节数。
	KeySize = 32
	// currentVersion 当前密文版本前缀。
	currentVersion = "v1"
)

// Box AES-256-GCM 加解密器，构造后并发安全（只读 aead）。
type Box struct {
	aead cipher.AEAD
}

// NewFromBase64 用 base64 编码的密钥构造（密钥从环境变量注入，禁止写死在代码里）。
func NewFromBase64(encodedKey string) (*Box, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("数据加密密钥不是合法的 base64: %w", err)
	}
	return New(raw)
}

// New 用原始密钥字节构造；长度必须为 32 字节（AES-256）。
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("数据加密密钥长度必须为 %d 字节，当前 %d 字节", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt 加密字符串；空串返回空串。
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	// Seal 把密文追加在 nonce 之后，整体 base64 存储。
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return currentVersion + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 Encrypt 产出的密文；空串返回空串。
// 仅限服务端核验/审计使用，禁止把结果写入日志或接口响应。
func (b *Box) Decrypt(sealed string) (string, error) {
	sealed = strings.TrimSpace(sealed)
	if sealed == "" {
		return "", nil
	}
	parts := strings.SplitN(sealed, ":", 2)
	if len(parts) != 2 {
		return "", errors.New("密文格式不合法")
	}
	if parts[0] != currentVersion {
		return "", fmt.Errorf("不支持的密文版本 %q", parts[0])
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("密文不是合法的 base64: %w", err)
	}
	nonceSize := b.aead.NonceSize()
	if len(raw) < nonceSize+b.aead.Overhead() {
		return "", errors.New("密文长度不合法")
	}
	plain, err := b.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		// 统一话术：不区分「密钥不对」与「密文被篡改」，避免侧信道提示。
		return "", errors.New("密文校验失败")
	}
	return string(plain), nil
}
