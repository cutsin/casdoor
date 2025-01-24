package pp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"os"
	"time"

	"github.com/casdoor/casdoor/util"
	"github.com/stretchr/testify/assert"
)

func TestNewAirwallexPaymentProvider(t *testing.T) {
	clientId := os.Getenv("AIRWALLEX_CLIENT_ID")
	apiKey := os.Getenv("AIRWALLEX_API_KEY")

	if clientId == "" || apiKey == "" {
		t.Skip("Skipping test: AIRWALLEX_CLIENT_ID or AIRWALLEX_API_KEY not set")
	}

	provider, err := NewAirwallexPaymentProvider(clientId, apiKey)
	assert.Nil(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, clientId, provider.ClientId)
	assert.Equal(t, apiKey, provider.APIKey)
	assert.Equal(t, "https://api.airwallex.com", provider.APIEndpoint)
	assert.NotNil(t, provider.client)
}

func TestGetAccessToken(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		if r.Header.Get("x-client-id") != "test-client-id" {
			t.Error("Expected client id header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Error("Expected api key header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// 设置30分钟后的过期时间
		expiresAt := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)

		// 返回成功响应
		resp := map[string]interface{}{
			"token":      "test-token",
			"expires_at": expiresAt,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 创建支付提供者实例
	pp, err := NewAirwallexPaymentProvider("test-client-id", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create payment provider: %v", err)
	}
	pp.APIEndpoint = server.URL

	// 测试获取token
	token, err := pp.getAccessToken()
	if err != nil {
		t.Fatalf("Failed to get access token: %v", err)
	}
	if token != "test-token" {
		t.Errorf("Expected token 'test-token', got '%s'", token)
	}

	// 测试token缓存
	cachedToken, err := pp.getAccessToken()
	if err != nil {
		t.Fatalf("Failed to get cached token: %v", err)
	}
	if cachedToken != token {
		t.Error("Cached token does not match original token")
	}

	// 测试错误情况
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid credentials",
		})
	}))
	defer errorServer.Close()

	pp.APIEndpoint = errorServer.URL
	pp.tokenCache = nil // 清除缓存
	_, err = pp.getAccessToken()
	if err == nil {
		t.Error("Expected error for invalid credentials")
	}
}

func TestGetAccessTokenSimple(t *testing.T) {
	clientId := os.Getenv("AIRWALLEX_CLIENT_ID")
	apiKey := os.Getenv("AIRWALLEX_API_KEY")

	if clientId == "" || apiKey == "" {
		t.Skip("Skipping test: AIRWALLEX_CLIENT_ID or AIRWALLEX_API_KEY not set")
	}

	pp, _ := NewAirwallexPaymentProvider(clientId, apiKey)
	token, err := pp.getAccessToken()

	fmt.Printf("Token result: %v\n", token)
	fmt.Printf("Error: %v\n", err)

	assert.NotEmpty(t, token)
	assert.Nil(t, err)
}

func TestPay(t *testing.T) {
	provider := setupTestProvider(t)
	if provider == nil {
		return
	}

	req := &PayReq{
		PaymentName:        util.GenerateId(),
		ProductName:        "monthly_memeber",
		ProductDisplayName: "月会员",
		ProductDescription: "月額会員 カスタムデジタルアバターなし",
		ProductImage:       "https://app-jp.psyai.com/images/promote/membership-demo.png",
		Price:              0.01,
		Currency:           "USD",
		ReturnUrl:          "https://app-jp.psyai.com/",
		ProviderName:       "awx",
	}

	t.Logf("Making payment request with: %+v", req)
	resp, err := provider.Pay(req)
	if err != nil {
		t.Logf("Payment error: %v", err)
		t.Fatal(err)
	}

	t.Logf("Got response: %+v", resp)
	assert.NotEmpty(t, resp.OrderId)
	assert.NotEmpty(t, resp.PayUrl)

	t.Logf("Payment URL: %s", resp.PayUrl)
	t.Logf("Order ID: %s", resp.OrderId)
}

func TestNotify(t *testing.T) {
	provider := setupTestProvider(t)
	if provider == nil {
		return
	}

	testCases := []struct {
		name           string
		status         string
		expectedStatus PaymentState
	}{
		{"success", "SUCCEEDED", PaymentStatePaid},
		{"failed", "FAILED", PaymentStateError},
		{"cancelled", "CANCELLED", PaymentStateError},
		{"expired", "EXPIRED", PaymentStateTimeout},
		{"pending", "PENDING", PaymentStateCreated},
		{"requires_payment", "REQUIRES_PAYMENT_METHOD", PaymentStateCreated},
		{"requires_action", "REQUIRES_CUSTOMER_ACTION", PaymentStateCreated},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			notification := map[string]interface{}{
				"status":     tc.status,
				"amount":     0.01,
				"currency":   "USD",
				"descriptor": joinAttachString([]string{"Test Product", "test_order_123", "test_provider"}),
			}
			body, _ := json.Marshal(notification)
			result, err := provider.Notify(body, "test_order_id")

			assert.Nil(t, err)
			assert.Equal(t, tc.expectedStatus, result.PaymentStatus)
			assert.Equal(t, "test_order_id", result.OrderId)
		})
	}
}

func TestGetResponseError(t *testing.T) {
	provider := setupTestProvider(t)
	if provider == nil {
		return
	}

	testCases := []struct {
		name          string
		err           error
		expectedError string
	}{
		{"nil error", nil, "success"},
		{"auth error", fmt.Errorf("invalid token in response"), "authentication_failed"},
		{"url error", fmt.Errorf("invalid payment URL in response"), "invalid_payment_url"},
		{"order error", fmt.Errorf("invalid order ID in response"), "invalid_order_id"},
		{"other error", fmt.Errorf("some other error"), "fail"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := provider.GetResponseError(tc.err)
			assert.Equal(t, tc.expectedError, result)
		})
	}
}

func TestTokenCache(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expiresAt := time.Now().Add(30 * time.Minute).UTC().Format("2006-01-02T15:04:05+0000")
		resp := map[string]string{
			"token":      "test-token",
			"expires_at": expiresAt,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 创建支付提供者实例
	pp, err := NewAirwallexPaymentProvider("test-client-id", "test-api-key")
	if err != nil {
		t.Fatalf("Failed to create payment provider: %v", err)
	}
	pp.APIEndpoint = server.URL

	// 第一次获取 token
	token1, err := pp.getAccessToken()
	if err != nil {
		t.Fatalf("Failed to get first token: %v", err)
	}
	if token1 != "test-token" {
		t.Errorf("Expected token 'test-token', got '%s'", token1)
	}

	// 立即再次获取 token，应该返回缓存的值
	token2, err := pp.getAccessToken()
	if err != nil {
		t.Fatalf("Failed to get cached token: %v", err)
	}
	if token2 != token1 {
		t.Error("Cached token does not match original token")
	}

	// 模拟过期
	pp.tokenCache.parsedExpiresAt = time.Now().Add(-time.Hour)

	// 获取新 token，应该刷新缓存
	token3, err := pp.getAccessToken()
	if err != nil {
		t.Fatalf("Failed to get new token after expiry: %v", err)
	}
	if token3 != "test-token" {
		t.Errorf("Expected new token 'test-token', got '%s'", token3)
	}
}

// Helper function to setup test provider
func setupTestProvider(t *testing.T) *AirwallexPaymentProvider {
	clientId := os.Getenv("AIRWALLEX_CLIENT_ID")
	apiKey := os.Getenv("AIRWALLEX_API_KEY")

	if clientId == "" || apiKey == "" {
		t.Skip("Skipping test: AIRWALLEX_CLIENT_ID or AIRWALLEX_API_KEY not set")
		return nil
	}

	provider, err := NewAirwallexPaymentProvider(clientId, apiKey)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
		return nil
	}

	return provider
}
