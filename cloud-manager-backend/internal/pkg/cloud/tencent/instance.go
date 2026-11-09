package tencent

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RenameInstance 调用腾讯云 CVM ModifyInstancesAttribute 修改实例名称（本地维护，走 TC3 签名）
func RenameInstance(secretID, secretKey, region, instanceID, name string) error {
	host := "cvm.tencentcloudapi.com"
	payloadBytes, _ := json.Marshal(map[string]interface{}{
		"InstanceIds":  []string{instanceID},
		"InstanceName": name,
	})
	payload := string(payloadBytes)

	now := time.Now().UTC()
	timestamp := now.Unix()
	date := now.Format("2006-01-02")
	service := "cvm"

	hashedPayload := sha256hex(payload)
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		"POST", "/", "",
		canonicalHeaders, signedHeaders, hashedPayload,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256hex(canonicalRequest)
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		hashedCanonicalRequest,
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, credentialScope, signedHeaders, signature)

	req, err := http.NewRequest("POST", "https://"+host+"/", strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", "ModifyInstancesAttribute")
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Version", "2017-03-12")
	req.Header.Set("X-TC-Region", region)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用腾讯云CVM失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		Response struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("解析腾讯云CVM响应失败: %w", err)
	}
	if out.Response.Error != nil {
		return fmt.Errorf("腾讯云CVM错误[%s]: %s", out.Response.Error.Code, out.Response.Error.Message)
	}
	return nil
}
