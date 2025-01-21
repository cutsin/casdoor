package pp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/casdoor/casdoor/util"
	// "github.com/casdoor/casdoor/util"
	// "io/ioutil"
	// "github.com/casdoor/casdoor/conf"
)

type AirwallexPaymentProvider struct {
	ClientId    string
	APIKey      string
	APIEndpoint string
	client      *http.Client
	tokenCache  *tokenInfo
	tokenMutex  sync.RWMutex
}

type tokenInfo struct {
	Token           string `json:"token"`
	ExpiresAt       string `json:"expires_at"`
	parsedExpiresAt time.Time
}

func NewAirwallexPaymentProvider(clientId string, apiKey string) (*AirwallexPaymentProvider, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	pp := &AirwallexPaymentProvider{
		ClientId:    clientId,
		APIKey:      apiKey,
		APIEndpoint: "https://api.airwallex.com",
		client:      client,
	}
	return pp, nil
}

func (pp *AirwallexPaymentProvider) Pay(req *PayReq) (*PayResp, error) {
	token, err := pp.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %v", err)
	}

	// Create payment intent
	intentReq := map[string]interface{}{
		"request_id":        util.GenerateId(),
		"amount":            req.Price,
		"currency":          req.Currency,
		"merchant_order_id": req.ProductName,
		"descriptor":        joinAttachString([]string{req.ProductDisplayName, req.ProductName, req.ProviderName}),
	}

	intentUrl := fmt.Sprintf("%s/api/v1/pa/payment_intents/create", pp.APIEndpoint)
	intentResp, err := pp.doRequest("POST", intentUrl, token, intentReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %v", err)
	}

	// Create payment link
	linkReq := map[string]interface{}{
		"payment_intent_id": intentResp["id"],
		"return_url":        req.ReturnUrl,
		"title":             req.ProductDisplayName,
		"reusable":          false,
	}

	linkUrl := fmt.Sprintf("%s/api/v1/pa/payment_links/create", pp.APIEndpoint)
	linkResp, err := pp.doRequest("POST", linkUrl, token, linkReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment link: %v", err)
	}

	payUrl, ok := linkResp["url"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid payment URL in response: %v", linkResp)
	}

	orderId, ok := intentResp["id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid order ID in response: %v", intentResp)
	}

	return &PayResp{
		PayUrl:  payUrl,
		OrderId: orderId,
	}, nil
}

func (pp *AirwallexPaymentProvider) Notify(body []byte, orderId string) (*NotifyResult, error) {
	var notification map[string]interface{}
	if err := json.Unmarshal(body, &notification); err != nil {
		return nil, err
	}

	statusStr, ok := notification["status"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid status in notification")
	}

	paymentStatus := PaymentStateError
	switch statusStr {
	case "SUCCEEDED":
		paymentStatus = PaymentStatePaid
	case "FAILED", "CANCELLED":
		paymentStatus = PaymentStateError
	case "EXPIRED":
		paymentStatus = PaymentStateTimeout
	case "PENDING", "REQUIRES_PAYMENT_METHOD", "REQUIRES_CUSTOMER_ACTION":
		paymentStatus = PaymentStateCreated
	}

	amount, _ := notification["amount"].(float64)
	currency, _ := notification["currency"].(string)
	descriptor, _ := notification["descriptor"].(string)

	var productDisplayName, productName, providerName string
	if descriptor != "" {
		productDisplayName, productName, providerName, _ = parseAttachString(descriptor)
	}

	return &NotifyResult{
		PaymentName:        "Airwallex",
		PaymentStatus:      paymentStatus,
		NotifyMessage:      string(body),
		OrderId:            orderId,
		Price:              amount,
		Currency:           currency,
		ProductName:        productName,
		ProductDisplayName: productDisplayName,
		ProviderName:       providerName,
	}, nil
}

func (pp *AirwallexPaymentProvider) GetInvoice(paymentName, personName, personIdCard, personEmail, personPhone, invoiceType, invoiceTitle, invoiceTaxId string) (string, error) {
	return "", nil
}

func (pp *AirwallexPaymentProvider) GetResponseError(err error) string {
	if err == nil {
		return "success"
	}
	return "fail"
}

func (pp *AirwallexPaymentProvider) getAccessToken() (string, error) {
	pp.tokenMutex.Lock()
	defer pp.tokenMutex.Unlock()

	if pp.tokenCache != nil && time.Now().Before(pp.tokenCache.parsedExpiresAt) {
		return pp.tokenCache.Token, nil
	}

	url := fmt.Sprintf("%s/api/v1/authentication/login", pp.APIEndpoint)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", err
	}

	req.Header.Set("x-client-id", pp.ClientId)
	req.Header.Set("x-api-key", pp.APIKey)

	resp, err := pp.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result tokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Token == "" || result.ExpiresAt == "" {
		return "", fmt.Errorf("invalid response: missing token or expires_at")
	}

	expiresAt, err := time.Parse(time.RFC3339, strings.Replace(result.ExpiresAt, "+0000", "+00:00", 1))
	if err != nil {
		return "", fmt.Errorf("failed to parse expires_at: %v", err)
	}

	result.parsedExpiresAt = expiresAt
	pp.tokenCache = &result

	return result.Token, nil
}

func (pp *AirwallexPaymentProvider) doRequest(method, url, token string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := pp.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if resp.StatusCode >= 400 {
		errorMsg, _ := json.Marshal(result)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(errorMsg))
	}

	return result, nil
}
