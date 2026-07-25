package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !VerifyPassword("correct-horse-battery-staple", hash) {
		t.Fatal("VerifyPassword() rejected the correct password")
	}
	if VerifyPassword("incorrect-password", hash) {
		t.Fatal("VerifyPassword() accepted an incorrect password")
	}
}

func TestHashPasswordRejectsShortPassword(t *testing.T) {
	t.Parallel()

	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("HashPassword() accepted a short password")
	}
}