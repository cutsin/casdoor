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
	// Create a payment intent
	intent, err := pp.AirWallexIntentNew(r)
	if err != nil {
		return nil, err
	}
	// Create a checkout url
	payResp := &PayResp{
		PayUrl: fmt.Sprintf("%sintent_id=%s&client_secret=%s&mode=payment&currency=%s&amount=%v&sessionId=%s&successUrl=%s&failUrl=%s&logoUrl=%s",
			pp.CheckoutURL,
			intent.Id,
			intent.ClientSecret,
			r.Currency,
			r.Price,
			r.PaymentName,
			r.ReturnUrl,
			r.ReturnUrl,
			pp.getLogoUrl(r)),
		OrderId: intent.Id,
	}
	return payResp, nil
}

func (pp *AirwallexPaymentProvider) Notify(body []byte, orderId string) (*NotifyResult, error) {
	notifyResult := &NotifyResult{}
	intent, err := pp.AirWallexIntentGet(orderId)
	if err != nil {
		return nil, err
	}
	// Check intent status
	switch intent.Status {
	case "PENDING", "REQUIRES_PAYMENT_METHOD", "REQUIRES_CUSTOMER_ACTION", "REQUIRES_CAPTURE":
		// The Payment is waiting for the confirm request. This status is returned right after the PaymentIntent is created or the previous PaymentAttempt has failed or expired.
		notifyResult.PaymentStatus = PaymentStateCreated
		return notifyResult, nil
	case "SUCCEEDED":
		// skip
	case "CANCELLED":
		// The Payment has been canceled by your request. The payment is closed.
		notifyResult.PaymentStatus = PaymentStateCanceled
	case "EXPIRED":
		// The Payment has expired. No further processing will occur.
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
 * The following methods are Airwallex-specific implementations
 * for handling authentication, API requests and payment intents,
 * until the official library is available.
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

type AirWallexIntentInfo struct {
	Amount        float64                `json:"amount"`
	Currency      string                 `json:"currency"`
	Id            string                 `json:"id"`
	Status        string                 `json:"status"`
	Descriptor    string                 `json:"descriptor"`
	RequestId     string                 `json:"request_id"`
	PaymentStatus string                 `json:"payment_status"`
	Metadata      map[string]interface{} `json:"metadata"`
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
		"descriptor":        description, // Not working (That will be displayed to the customer)
		"metadata":          map[string]interface{}{"description": description},
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

// Get a payment intent
func (pp *AirwallexPaymentProvider) AirWallexIntentGet(intentId string) (*AirWallexIntentInfo, error) {
	token, err := pp.AirWallexTokenGet()
	if err != nil {
		return nil, err
	}
	intentUrl := fmt.Sprintf("%s/pa/payment_intents/%s", pp.APIEndpoint, intentId)
	intentRes, err := pp.doRequest("GET", intentUrl, token, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment intent: %v", err)
	}
	// Extract payment status from latest_payment_attempt if exists
	var paymentStatus string
	if attempt, ok := intentRes["latest_payment_attempt"].(map[string]interface{}); ok {
		if status, ok := attempt["status"].(string); ok {
			paymentStatus = status
		}
	}
	return &AirWallexIntentInfo{
		Amount:        intentRes["amount"].(float64),
		Currency:      intentRes["currency"].(string),
		Id:            intentRes["id"].(string),
		Status:        intentRes["status"].(string),
		Descriptor:    intentRes["descriptor"].(string),
		RequestId:     intentRes["request_id"].(string),
		PaymentStatus: paymentStatus,
		Metadata:      intentRes["metadata"].(map[string]interface{}),
	}, nil
}
