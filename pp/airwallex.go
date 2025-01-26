package pp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	// "io/ioutil"
	// "github.com/casdoor/casdoor/conf"
)

type AirwallexPaymentProvider struct {
	ClientId    string
	APIKey      string
	APIEndpoint string
	CheckoutURL string
	client      *http.Client
	tokenCache  *AirWallexTokenInfo
	tokenMutex  sync.RWMutex
}

func NewAirwallexPaymentProvider(clientId string, apiKey string) (*AirwallexPaymentProvider, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	pp := &AirwallexPaymentProvider{
		ClientId:    clientId,
		APIKey:      apiKey,
		APIEndpoint: "https://api.airwallex.com/api/v1",
		CheckoutURL: "https://checkout.airwallex.com/#/standalone/checkout?",
		client:      client,
	}
	return pp, nil
}

func (pp *AirwallexPaymentProvider) Pay(r *PayReq) (*PayResp, error) {
	intent, err := pp.AirWallexIntentNew(r)
	if err != nil {
		return nil, err
	}
	// Create a Checkout URL (ref: https://www.airwallex.com/docs/js/payments/hosted-payment-page/)
	p2 := map[string]interface{}{
		"intent_id":     intent.Id,
		"client_secret": intent.ClientSecret,
		"sessionId":     r.PaymentName,
		"currency":      r.Currency,
		"successUrl":    r.ReturnUrl,
		"failUrl":       r.ReturnUrl,
		"logoUrl":       pp.getLogoUrl(r), // Replace the potentially misleading Airwallex's logo.
	}
	params := url.Values{}
	for key, value := range p2 {
		params.Add(key, fmt.Sprintf("%v", value))
	}
	payResp := &PayResp{
		PayUrl:  pp.CheckoutURL + params.Encode(),
		OrderId: intent.Id,
	}
	return payResp, nil
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

/*
 * The following methods are Airwallex-specific implementations
 * for handling authentication, API requests and payment intents.
 */

type AirWallexIntentResp struct {
	Id           string `json:"id"`
	ClientSecret string `json:"client_secret"`
}

type AirWallexTokenInfo struct {
	Token           string `json:"token"`
	ExpiresAt       string `json:"expires_at"`
	parsedExpiresAt time.Time
}

func (pp *AirwallexPaymentProvider) AirWallexTokenGet() (string, error) {
	pp.tokenMutex.Lock()
	defer pp.tokenMutex.Unlock()

	if pp.tokenCache != nil && time.Now().Before(pp.tokenCache.parsedExpiresAt) {
		return pp.tokenCache.Token, nil
	}

	url := fmt.Sprintf("%s/authentication/login", pp.APIEndpoint)
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

	var result AirWallexTokenInfo
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

// Create a payment intent
func (pp *AirwallexPaymentProvider) AirWallexIntentNew(r *PayReq) (*AirWallexIntentResp, error) {
	token, err := pp.AirWallexTokenGet()
	if err != nil {
		return nil, err
	}
	description := joinAttachString([]string{r.ProductName, r.ProductDisplayName, r.ProviderName})
	intentReq := map[string]interface{}{
		"request_id":        r.PaymentName,
		"currency":          r.Currency,
		"amount":            r.Price,
		"merchant_order_id": r.PaymentName,
		"descriptor":        description, // Not working
		"metadata":          map[string]interface{}{"descriptor": description},
		"order":             map[string]interface{}{"products": []map[string]interface{}{{"name": r.ProductDisplayName, "quantity": 1, "desc": r.ProductDescription, "image_url": r.ProductImage}}},
	}
	intentUrl := fmt.Sprintf("%s/pa/payment_intents/create", pp.APIEndpoint)
	intentRes, err := pp.doRequest("POST", intentUrl, token, intentReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %v", err)
	}
	return &AirWallexIntentResp{
		Id:           intentRes["id"].(string),
		ClientSecret: intentRes["client_secret"].(string),
	}, nil
}

// Try to get the logo URL of the merchant's site
func (pp *AirwallexPaymentProvider) getLogoUrl(r *PayReq) string {
	if r.ReturnUrl == "" {
		return "data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs="
	}
	from, _ := url.Parse(r.ReturnUrl)
	return from.Host + "/favicon.ico"
}
