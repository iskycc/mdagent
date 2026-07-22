package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	callbackTimestampHeader = "X-MD-Timestamp"
	callbackSignatureHeader = "X-MD-Signature"
	callbackTimestampWindow = 5 * time.Minute
)

// signCallbackBody 计算回调请求签名。
// 签名为 HMAC-SHA256(secret, timestamp + "." + rawBody) 的十六进制字符串。
func signCallbackBody(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateCallbackSignature 校验回调请求签名。
// 未配置 secret 时跳过校验；配置了 secret 则必须提供正确的 X-MD-Timestamp 与 X-MD-Signature。
func validateCallbackSignature(secret string, r *http.Request, body []byte) error {
	if secret == "" {
		return nil
	}

	timestamp := r.Header.Get(callbackTimestampHeader)
	signature := r.Header.Get(callbackSignatureHeader)
	if timestamp == "" || signature == "" {
		return fmt.Errorf("missing callback signature headers")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	if time.Since(time.Unix(ts, 0)).Abs() > callbackTimestampWindow {
		return fmt.Errorf("timestamp out of window")
	}

	expected := signCallbackBody(secret, timestamp, body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
