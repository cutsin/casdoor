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
		APIEndpoint: "https://api.airwallex.com/api/v1",
		CheckoutURL: "https://checkout.airwallex.com/#/standalone/checkout?",
		client:      client,
	}
	return pp, nil
}

type checkoutReq struct {
	IntentId     string
	ClientSecret string
	Currency     string
	SuccessURL   string
	SessionId    string
	FailUrl      string
	LogoUrl      string
}
type productResp struct {
	description string
}

// Ref: https://github.com/airwallex/airwallex-payment-demo/blob/master/docs/hpp.md
func getCheckoutURL(p *checkoutReq, baseURL string) string {
	url := fmt.Sprintf("%scurrency=%s&intent_id=%s&client_secret=%s&sessionId=%s&successUrl=%s&failUrl=%s&logoUrl=%s",
		baseURL, p.Currency, p.IntentId, p.ClientSecret, p.SessionId, p.SuccessURL, p.FailUrl, p.LogoUrl)

	return url
}

type intentResp struct {
	Id           string `json:"id"`
	ClientSecret string `json:"client_secret"`
}

func getCheckoutURL2(pp *AirwallexPaymentProvider, intent *intentResp, req *PayReq) string {
	from, _ := url.Parse(req.ReturnUrl)

	params := checkoutReq{
		IntentId:     intent.Id,
		ClientSecret: intent.ClientSecret,
		Currency:     req.Currency,
		SessionId:    req.PaymentName,
		SuccessURL:   req.ReturnUrl,
		FailUrl:      req.ReturnUrl,
		LogoUrl:      from.Scheme + "://" + from.Host + "/favicon.ico", // replace Airwallex's default logo
	}

	return getCheckoutURL(&params, pp.CheckoutURL)
}

func (pp *AirwallexPaymentProvider) Pay(req *PayReq) (*PayResp, error) {
	token, err := pp.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %v", err)
	}
	orderId := req.PaymentName
	description := joinAttachString([]string{req.ProductName, req.ProductDisplayName, req.ProviderName})

	// Create payment intent
	intentReq := map[string]interface{}{
		"request_id":        orderId,
		"currency":          req.Currency,
		"amount":            req.Price,
		"merchant_order_id": orderId,
		"name":              req.ProductDisplayName,
		"descriptor":        description, // Not working as expected
		"metadata":          map[string]interface{}{"description": description},
		"order": map[string]interface{}{
			"products": []map[string]interface{}{
				{
					"name":       req.ProductDisplayName,
					"quantity":   1,
					"desc":       "1" + req.ProductDescription,
					"descriptor": description,
					"image_url":  req.ProductImage,
				},
			},
		},
	}
	fmt.Println("intentReq: ", intentReq)
	intentUrl := fmt.Sprintf("%s/pa/payment_intents/create", pp.APIEndpoint)
	intentRes, err := pp.doRequest("POST", intentUrl, token, intentReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %v", err)
	}

	// Convert map to intentResp
	intent := &intentResp{
		Id:           intentRes["id"].(string),
		ClientSecret: intentRes["client_secret"].(string),
	}

	// Create a checkout url (ref: https://www.airwallex.com/docs/js/payments/hosted-payment-page/)
	params := checkoutReq{
		IntentId:     intent.Id,
		ClientSecret: intent.ClientSecret,
		Currency:     req.Currency,
		SessionId:    orderId,
		SuccessURL:   req.ReturnUrl,
		FailUrl:      req.ReturnUrl,
		// LogoUrl:      from + "/favicon.ico", // replace Airwallex's default logo
	}

	payResp := &PayResp{
		PayUrl:  getCheckoutURL2(pp, intent, req),
		OrderId: params.IntentId,
	}
	return payResp, nil
}

// FailURL: http://localhost/return?error=default_backend_error&id=int_sgpdpllrch3ykrmtx6f&type=FAIL_URL
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
