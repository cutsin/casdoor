package pp

import (
    "bytes"
    "encoding/json"
    "fmt"
    // "io/ioutil"
    "net/http"
    // "time"
	"github.com/casdoor/casdoor/conf"
)

type AirwallexPaymentProvider struct {
    ClientId     string
    APIKey       string
    APIEndpoint  string
    isProd       bool
}

func NewAirwallexPaymentProvider(clientId, apiKey string) (*AirwallexPaymentProvider, error) {
    isProd := false
    if conf.GetConfigString("runmode") == "prod" {
        isProd = true
    }
    
    endpoint := "https://api-demo.airwallex.com"
    if isProd {
        endpoint = "https://api.airwallex.com"
    }

    return &AirwallexPaymentProvider{
        ClientId:    clientId,
        APIKey:      apiKey,
        APIEndpoint: endpoint,
        isProd:      isProd,
    }, nil
}

func (pp *AirwallexPaymentProvider) Pay(req *PayReq) (*PayResp, error) {
    token, err := pp.getAccessToken()
    if err != nil {
        return nil, err
    }

    // Create payment intent
    intentReq := map[string]interface{}{
        "amount":           req.Price,
        "currency":         req.Currency,
        "merchant_order_id": req.ProductName,
        "descriptor":       req.ProductDisplayName,
        "return_url":       req.ReturnUrl,
        "metadata": map[string]string{
            "product_name": req.ProductName,
            "payer_id":     req.PayerId,
        },
    }

    intentUrl := fmt.Sprintf("%s/api/v1/pa/payment_intents/create", pp.APIEndpoint)
    intentResp, err := pp.doRequest("POST", intentUrl, token, intentReq)
    if err != nil {
        return nil, err
    }

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

    return &PayResp{
        PayUrl:  linkResp["url"].(string),
        OrderId: intentResp["id"].(string),
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
    case "FAILED":
        status = PaymentStateError
    case "PENDING":
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
    
    req, err := http.NewRequest("POST", url, nil)
    if err != nil {
        return "", err
    }

    req.Header.Set("x-client-id", pp.ClientId)
    req.Header.Set("x-api-key", pp.APIKey)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    return result["token"].(string), nil
}

func (pp *AirwallexPaymentProvider) doRequest(method, url, token string, body interface{}) (map[string]interface{}, error) {
    jsonBody, err := json.Marshal(body)
    if err != nil {
        return nil, err
    }

    req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, err
    }

    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
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