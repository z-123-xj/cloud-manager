package tencent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GetAccountID 通过 STS DescribeAccountInfo 获取腾讯云主账号 UIN（account_id）
// 使用 TC3-HMAC-SHA256 签名，仅依赖标准库，避免引入额外的 SDK 子模块依赖。
func GetAccountID(secretID, secretKey string) (string, error) {
	host := "sts.tencentcloudapi.com"
	action := "DescribeAccountInfo"
	service := "sts"
	version := "2018-03-01"
	payload := "{}"

	now := time.Now().UTC()
	timestamp := now.Unix()
	date := now.Format("2006-01-02")

	// 1. 规范请求串
	hashedPayload := sha256hex(payload)
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")

	// 2. 待签名字符串
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256hex(canonicalRequest)
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		hashedCanonicalRequest,
	}, "\n")

	// 3. 计算签名
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, credentialScope, signedHeaders, signature)

	// 4. 发起请求
	req, err := http.NewRequest("POST", "https://"+host+"/", strings.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Version", version)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用腾讯云STS失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		Response struct {
			OwnerUin  int64 `json:"OwnerUin"`
			Error     *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
			RequestId string `json:"RequestId"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("解析腾讯云STS响应失败: %w", err)
	}
	if out.Response.Error != nil {
		return "", fmt.Errorf("腾讯云STS错误[%s]: %s", out.Response.Error.Code, out.Response.Error.Message)
	}
	if out.Response.OwnerUin == 0 {
		return "", fmt.Errorf("腾讯云STS未返回账号UIN")
	}
	return fmt.Sprintf("%d", out.Response.OwnerUin), nil
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
