package testdata

import (
	"crypto/md5"
	"fmt"
)

// HashPassword is a TEST FIXTURE using weak cryptography (MD5).
func HashPassword(password string) string {
	h := md5.Sum([]byte(password))
	return fmt.Sprintf("%x", h)
}
