package securebox

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// testKey 32 字节测试密钥（仅测试使用，禁止在生产复用）。
var testKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := NewFromBase64(testKey)
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	cases := []string{"张三", "11010519491231002X", "a"}
	for _, plain := range cases {
		sealed, err := box.Encrypt(plain)
		if err != nil {
			t.Fatalf("加密 %q 失败: %v", plain, err)
		}
		if !strings.HasPrefix(sealed, "v1:") {
			t.Fatalf("密文应带版本前缀，实际: %q", sealed)
		}
		// 明文检查要在解码后的字节上做：base64 文本本身可能恰好包含单字符明文（如 "a"）。
		// 单字节明文在随机密文中天然有概率出现，只对多字节明文做「不包含」断言。
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
		if err != nil {
			t.Fatalf("密文不是合法 base64: %v", err)
		}
		if len(plain) >= 4 && bytes.Contains(raw, []byte(plain)) {
			t.Fatalf("密文不应包含明文，实际: %q", sealed)
		}
		got, err := box.Decrypt(sealed)
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if got != plain {
			t.Fatalf("解密结果不一致: want %q got %q", plain, got)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	box, err := NewFromBase64(testKey)
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	first, err := box.Encrypt("张三")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	second, err := box.Encrypt("张三")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if first == second {
		t.Fatal("相同明文两次加密结果不应相同（nonce 必须随机）")
	}
}

func TestDecryptRejectsTampered(t *testing.T) {
	box, err := NewFromBase64(testKey)
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	sealed, err := box.Encrypt("张三")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	// 篡改最后一个字符，GCM 校验必须失败。
	tampered := sealed[:len(sealed)-1] + "A"
	if _, err := box.Decrypt(tampered); err == nil {
		t.Fatal("篡改后的密文应解密失败")
	}
}

func TestNewRejectsWrongKeySize(t *testing.T) {
	if _, err := New([]byte("short-key")); err == nil {
		t.Fatal("非 32 字节密钥应被拒绝")
	}
	if _, err := NewFromBase64("not-base64!!"); err == nil {
		t.Fatal("非法 base64 应被拒绝")
	}
}

func TestEmptyValueRoundTrip(t *testing.T) {
	box, err := NewFromBase64(testKey)
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	if sealed, err := box.Encrypt(""); err != nil || sealed != "" {
		t.Fatalf("空串应原样返回: %q %v", sealed, err)
	}
	if plain, err := box.Decrypt(""); err != nil || plain != "" {
		t.Fatalf("空密文应返回空串: %q %v", plain, err)
	}
}
