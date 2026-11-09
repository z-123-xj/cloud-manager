package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// defaultSecretKey 内置默认密钥（公开占位值，仅作兼容回退）。
// 注意：该值随源码公开，任何人可解密历史数据，生产环境必须通过环境变量覆盖！
const defaultSecretKey = "your-32-byte-secret-key-here!!!!" // ⚠️ 必须 32 字节

// getSecretKey 获取 AES-256 密钥（必须恰好 32 字节），优先级：
//  1. 环境变量 AES_SECRET_KEY（生产推荐）：长度必须为 32 字节，否则启动 panic
//  2. 内置默认密钥：仅用于兼容环境变量未设置时的历史加密数据
//
// 部署方式: export AES_SECRET_KEY="your-32-byte-secret-key-here!!!!"
func getSecretKey() []byte {
	key := os.Getenv("AES_SECRET_KEY")
	if key == "" {
		// 未设置环境变量，回退内置密钥（兼容历史数据）
		return []byte(defaultSecretKey)
	}
	if len(key) != 32 {
		panic(fmt.Sprintf("FATAL: AES_SECRET_KEY 长度必须为 32 字节，当前为 %d 字节", len(key)))
	}
	return []byte(key)
}

// secretKey 在包初始化时从环境变量加载（未设置时回退内置默认密钥）
var secretKey = getSecretKey()

// Encrypt 加密
func Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}

	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))

	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密
func Decrypt(ciphertextBase64 string) (string, error) {
	ciphertext, err := base64.URLEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < aes.BlockSize {
		return "", errors.New("ciphertext too short")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)

	return string(ciphertext), nil
}
