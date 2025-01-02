package pp

import (
	"bytes"
	"encoding/json"
	"fmt"

	// "io/ioutil"
	"net/http"
	// "time"
	// "github.com/casdoor/casdoor/conf"
	"github.com/casdoor/casdoor/util"
)

type AirwallexPaymentProvider struct {
	ClientId    string
	APIKey      string
	APIEndpoint string
}

func NewAirwallexPaymentProvider(clientId, apiKey string) (*AirwallexPaymentProvider, error) {
	endpoint := "https://api.airwallex.com"

	return &AirwallexPaymentProvider{
		ClientId:    clientId,
		APIKey:      apiKey,
		APIEndpoint: endpoint,
	}, nil
}

func (pp *AirwallexPaymentProvider) Pay(req *PayReq) (*PayResp, error) {
	token, err := pp.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	// Create payment intent
	intentReq := map[string]interface{}{
		"request_id":        util.GenerateId(),
		"amount":            req.Price,
		"currency":          req.Currency,
		"merchant_order_id": req.ProductName,
	}

	intentUrl := fmt.Sprintf("%s/api/v1/pa/payment_intents/create", pp.APIEndpoint)
	intentResp, err := pp.doRequest("POST", intentUrl, token, intentReq)
	if err != nil {
		return nil, err
	}
	fmt.Println(intentResp)

	// Create payment link
	linkReq := map[string]interface{}{
		"payment_intent_id": intentResp["id"],
		"return_url":        req.ReturnUrl,
	}

	linkUrl := fmt.Sprintf("%s/api/v1/pa/payment_links/create", pp.APIEndpoint)
	linkResp, err := pp.doRequest("POST", linkUrl, token, linkReq)
	if err != nil {
		return nil, err
	}

	// Add validation for required fields
	payUrl, ok := linkResp["url"]
	if !ok || payUrl == nil {
		return nil, fmt.Errorf("payment URL is missing from response: %v", linkResp)
	}

	orderId, ok := intentResp["id"]
	if !ok || orderId == nil {
		return nil, fmt.Errorf("order ID is missing from response: %v", intentResp)
	}

	return &PayResp{
		PayUrl:  payUrl.(string),
		OrderId: orderId.(string),
	}, nil
}

func (pp *AirwallexPaymentProvider) Notify(body []byte, orderId string) (*NotifyResult, error) {
	var notification map[string]interface{}
	if err := json.Unmarshal(body, &notification); err != nil {
		return nil, err
	}

	status := PaymentStateError // default to error state
	switch notification["status"].(string) {
	case "SUCCEEDED":
		status = PaymentStatePaid
	case "FAILED", "CANCELLED":
		status = PaymentStateError
	case "PENDING", "REQUIRES_PAYMENT_METHOD", "REQUIRES_CUSTOMER_ACTION":
		status = PaymentStateCreated
	}

	return &NotifyResult{
		PaymentName:   "Airwallex",
		PaymentStatus: status,
		NotifyMessage: string(body),
		OrderId:       orderId,
	}, nil
}

func (pp *AirwallexPaymentProvider) GetInvoice(paymentName, personName, personIdCard, personEmail, personPhone, invoiceType, invoiceTitle, invoiceTaxId string) (string, error) {
	return "", nil
}

func (pp *AirwallexPaymentProvider) GetResponseError(err error) string {
	return err.Error()
}

func (pp *AirwallexPaymentProvider) getAccessToken() (string, error) {
	url := fmt.Sprintf("%s/api/v1/authentication/login", pp.APIEndpoint)

	// Create empty JSON body
	emptyBody := bytes.NewBuffer([]byte("{}"))
	req, err := http.NewRequest("POST", url, emptyBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-client-id", pp.ClientId)
	req.Header.Set("x-api-key", pp.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	token, ok := result["token"]
	if !ok || token == nil {
		return "", fmt.Errorf("token not found in response: %v", result)
	}

	tokenStr, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("token is not a string: %v", token)
	}

	return tokenStr, nil
}

func (pp *AirwallexPaymentProvider) doRequest(method, url, token string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Generate request ID
	requestId := util.GenerateId()

	// Add request_id to the request body
	var requestBody map[string]interface{}
	if err := json.Unmarshal(jsonBody, &requestBody); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request body: %w", err)
	}
	requestBody["request_id"] = requestId

	// Re-marshal the body with request_id
	jsonBody, err = json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body with request_id: %w", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	fmt.Println("_____________", req)
	fmt.Println(client)
	resp, err := client.Do(req)
	fmt.Println(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed with status %d: %v", resp.StatusCode, result)
	}

	return result, nil
}
