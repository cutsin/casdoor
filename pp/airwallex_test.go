package pp

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
)

func setupMockServer() *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/api/v1/authentication/login":
            json.NewEncoder(w).Encode(map[string]interface{}{
                "token": "mock_token",
            })
        case "/api/v1/pa/payment_intents/create":
            json.NewEncoder(w).Encode(map[string]interface{}{
                "id": "mock_intent_id",
            })
        case "/api/v1/pa/payment_links/create":
            json.NewEncoder(w).Encode(map[string]interface{}{
                "url": "https://checkout.mock.com/pay",
            })
        }
    }))
}

func TestPay(t *testing.T) {
    server := setupMockServer()
    defer server.Close()

    provider, err := NewAirwallexPaymentProvider("test_id", "test_key")
    assert.Nil(t, err)
    provider.APIEndpoint = server.URL

    req := &PayReq{
        ProductName:        "test_product",
        ProductDisplayName: "Test Product",
        Price:             100.00,
        Currency:          "USD",
        ReturnUrl:         "http://localhost/return",
        PayerId:           "test_payer",
    }

    resp, err := provider.Pay(req)
    assert.Nil(t, err)
    assert.Equal(t, "mock_intent_id", resp.OrderId)
    assert.Equal(t, "https://checkout.mock.com/pay", resp.PayUrl)
}

func TestNotify(t *testing.T) {
    provider, _ := NewAirwallexPaymentProvider("test_id", "test_key")

    testCases := []struct {
        name           string
        notifyBody    string
        expectedState PaymentState
    }{
        {
            name:        "success payment",
            notifyBody: `{"status": "SUCCEEDED"}`,
            expectedState: PaymentStatePaid,
        },
        {
            name:        "failed payment",
            notifyBody: `{"status": "FAILED"}`,
            expectedState: PaymentStateError,
        },
        {
            name:        "pending payment",
            notifyBody: `{"status": "PENDING"}`,
            expectedState: PaymentStateCreated,
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            result, err := provider.Notify([]byte(tc.notifyBody), "test_order")
            assert.Nil(t, err)
            assert.Equal(t, tc.expectedState, result.PaymentStatus)
        })
    }
}