package data

import "testing"

func TestCentsScan(t *testing.T) {
	var value cents
	if err := value.Scan("12.60"); err != nil {
		t.Fatal(err)
	}
	if value != 1260 {
		t.Fatalf("expected 1260, actual %d", value)
	}
}

func TestEncryptPhoneDoesNotStorePlaintext(t *testing.T) {
	const phone = "13812345678"
	encrypted, err := encryptPhone("test-only-random-key-with-enough-length", phone)
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) == phone {
		t.Fatal("encrypted phone must not equal plaintext")
	}
	if maskPhone(phone) != "138****5678" {
		t.Fatalf("unexpected masked phone: %s", maskPhone(phone))
	}
}
