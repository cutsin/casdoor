package pp

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "os"
)

func TestPay(t *testing.T) {
    clientId := os.Getenv("AIRWALLEX_CLIENT_ID")
    apiKey := os.Getenv("AIRWALLEX_API_KEY")
    
    if clientId == "" || apiKey == "" {
        t.Skip("Skipping test: AIRWALLEX_CLIENT_ID or AIRWALLEX_API_KEY not set")
    }

    provider, err := NewAirwallexPaymentProvider(clientId, apiKey)
    if err != nil {
        t.Fatalf("Failed to create provider: %v", err)
    }
    assert.Nil(t, err)

    req := &PayReq{
        ProductName:        "test_order_123",
        ProductDisplayName: "Test Product",
        Price:             0.01,
        Currency:          "USD",
        ReturnUrl:         "http://localhost/return",
        PayerId:           "test_payer",
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