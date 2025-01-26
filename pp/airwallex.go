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
)

type AirwallexClient struct {
	ClientId    string
	APIKey      string
	APIEndpoint string
	APICheckout string
	client      *http.Client
	tokenCache  *AirWallexTokenInfo
	tokenMutex  sync.RWMutex
}

type AirwallexPaymentProvider struct {
	Client *AirwallexClient
}

func NewAirwallexPaymentProvider(clientId string, apiKey string) (*AirwallexPaymentProvider, error) {
	client := &AirwallexClient{
		ClientId:    clientId,
		APIKey:      apiKey,
		APIEndpoint: "https://api.airwallex.com/api/v1",
		APICheckout: "https://checkout.airwallex.com/#/standalone/checkout?",
		client:      &http.Client{Timeout: 10 * time.Second},
	}
	pp := &AirwallexPaymentProvider{
		Client: client,
	}
	return pp, nil
}

func (pp *AirwallexPaymentProvider) Pay(r *PayReq) (*PayResp, error) {
	// Create a payment intent
	intent, err := pp.Client.CreateIntent(r)
	if err != nil {
		return nil, err
	}
	payUrl, err := pp.Client.GetCheckoutUrl(intent, r)
	if err != nil {
		return nil, err
	}
	return &PayResp{
		PayUrl:  payUrl,
		OrderId: intent.Id,
	}, nil
}

func (pp *AirwallexPaymentProvider) Notify(body []byte, orderId string) (*NotifyResult, error) {
	notifyResult := &NotifyResult{}
	intent, err := pp.Client.GetIntent(orderId)
	if err != nil {
		return nil, err
	}
	// Check intent status
	switch intent.Status {
	case "PENDING", "REQUIRES_PAYMENT_METHOD", "REQUIRES_CUSTOMER_ACTION", "REQUIRES_CAPTURE":
		notifyResult.PaymentStatus = PaymentStateCreated
		return notifyResult, nil
	case "SUCCEEDED":
		// skip
	case "CANCELLED":
		notifyResult.PaymentStatus = PaymentStateCanceled
	case "EXPIRED":
		notifyResult.PaymentStatus = PaymentStateTimeout
	default:
		notifyResult.PaymentStatus = PaymentStateError
		notifyResult.NotifyMessage = fmt.Sprintf("unexpected airwallex checkout status: %v", intent.PaymentStatus+intent.Status)
		return notifyResult, nil
	}
	// Check attempt status
	switch intent.PaymentStatus {
	case "PAID", "SETTLED":
		// Skip
	case "CANCELLED", "EXPIRED", "RECEIVED", "AUTHENTICATION_REDIRECTED", "AUTHORIZED", "CAPTURE_REQUESTED":
		notifyResult.PaymentStatus = PaymentStateCreated
		return notifyResult, nil
	default:
		notifyResult.PaymentStatus = PaymentStateError
		notifyResult.NotifyMessage = fmt.Sprintf("unexpected airwallex checkout payment status: %v", intent.PaymentStatus+intent.Status)
		return notifyResult, nil
	}

	// The Payment has succeeded.
	var productDisplayName, productName, providerName string
	if description, ok := intent.Metadata["description"]; ok {
		productName, productDisplayName, providerName, _ = parseAttachString(description.(string))
	}
	return &NotifyResult{
		PaymentName:        intent.RequestId,
		PaymentStatus:      PaymentStatePaid,
		ProductName:        productName,
		ProductDisplayName: productDisplayName,
		ProviderName:       providerName,
		Price:              intent.Amount,
		Currency:           intent.Currency,
		OrderId:            intent.RequestId,
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
 * Airwallex Client implementation
 */

type AirWallexTokenInfo struct {
	Token           string `json:"token"`
	ExpiresAt       string `json:"expires_at"`
	parsedExpiresAt time.Time
}

type AirWallexIntentResp struct {
	Id           string `json:"id"`
	ClientSecret string `json:"client_secret"`
}

type AirWallexIntentInfo struct {
	Amount        float64                `json:"amount"`
	Currency      string                 `json:"currency"`
	Id            string                 `json:"id"`
	Status        string                 `json:"status"`
	Descriptor    *string                `json:"descriptor,omitempty"`
	RequestId     string                 `json:"request_id"`
	PaymentStatus string                 `json:"payment_status"`
	Metadata      map[string]interface{} `json:"metadata"`
}

func (c *AirwallexClient) GetToken() (string, error) {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()

	if c.tokenCache != nil && time.Now().Before(c.tokenCache.parsedExpiresAt) {
		return c.tokenCache.Token, nil
	}

	url := fmt.Sprintf("%s/authentication/login", c.APIEndpoint)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", err
	}

	req.Header.Set("x-client-id", c.ClientId)
	req.Header.Set("x-api-key", c.APIKey)

	resp, err := c.client.Do(req)
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
	c.tokenCache = &result

	return result.Token, nil
}

func (c *AirwallexClient) authRequest(method, url string, body interface{}) (map[string]interface{}, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewBuffer(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *AirwallexClient) CreateIntent(r *PayReq) (*AirWallexIntentResp, error) {
	description := joinAttachString([]string{r.ProductName, r.ProductDisplayName, r.ProviderName})
	intentReq := map[string]interface{}{
		"request_id":        r.PaymentName,
		"currency":          r.Currency,
		"amount":            r.Price,
		"merchant_order_id": r.PaymentName,
		"descriptor":        description,
		"metadata":          map[string]interface{}{"description": description},
		"order":             map[string]interface{}{"products": []map[string]interface{}{{"name": r.ProductDisplayName, "quantity": 1, "desc": r.ProductDescription, "image_url": r.ProductImage}}},
	}
	intentUrl := fmt.Sprintf("%s/pa/payment_intents/create", c.APIEndpoint)
	intentRes, err := c.authRequest("POST", intentUrl, intentReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %v", err)
	}
	return &AirWallexIntentResp{
		Id:           intentRes["id"].(string),
		ClientSecret: intentRes["client_secret"].(string),
	}, nil
}

func (c *AirwallexClient) GetIntent(intentId string) (*AirWallexIntentInfo, error) {
	intentUrl := fmt.Sprintf("%s/pa/payment_intents/%s", c.APIEndpoint, intentId)
	intentRes, err := c.authRequest("GET", intentUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment intent: %v", err)
	}
	// Extract payment status from latest_payment_attempt if exists
	var paymentStatus string
	if attempt, ok := intentRes["latest_payment_attempt"].(map[string]interface{}); ok && attempt != nil {
		if status, ok := attempt["status"].(string); ok {
			paymentStatus = status
		}
	}
	metadata := make(map[string]interface{})
	if meta, ok := intentRes["metadata"].(map[string]interface{}); ok && meta != nil {
		metadata = meta
	}
	var descriptor *string
	if desc, ok := intentRes["descriptor"].(string); ok {
		descriptor = &desc
	}

	return &AirWallexIntentInfo{
		Amount:        intentRes["amount"].(float64),
		Currency:      intentRes["currency"].(string),
		Id:            intentRes["id"].(string),
		Status:        intentRes["status"].(string),
		Descriptor:    descriptor,
		RequestId:     intentRes["request_id"].(string),
		PaymentStatus: paymentStatus,
		Metadata:      metadata,
	}, nil
}

func (c *AirwallexClient) GetCheckoutUrl(intent *AirWallexIntentResp, r *PayReq) (string, error) {
	// Try to get the logo URL of the merchant's site (replace the Airwallex's default logo.)
	logoUrl := "data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs="
	if r.ReturnUrl != "" {
		from, _ := url.Parse(r.ReturnUrl)
		logoUrl = "//" + from.Host + "/favicon.ico"
	}
	return fmt.Sprintf("%sintent_id=%s&client_secret=%s&mode=payment&currency=%s&amount=%v&sessionId=%s&successUrl=%s&failUrl=%s&logoUrl=%s",
		c.APICheckout,
		intent.Id,
		intent.ClientSecret,
		r.Currency,
		r.Price,
		r.PaymentName,
		r.ReturnUrl,
		r.ReturnUrl,
		logoUrl), nil
}
