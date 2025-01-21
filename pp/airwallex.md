# 支付接口规范文档

## 通用接口定义

### 1. 初始化配置
```go
// Stripe 配置
type StripePaymentProvider struct {
    PublishableKey string
    SecretKey      string
    isProd         bool
}

// Airwallex 配置
type AirwallexPaymentProvider struct {
    ClientId     string
    APIKey       string
    APIEndpoint  string
    client       *http.Client
}
```

### 2. 支付相关接口

#### 2.1 创建支付（Pay）
- 接口功能：创建支付订单并获取支付链接
- 请求参数：
  ```go
  type PayReq struct {
      ProductName        string
      ProductDisplayName string
      ProviderName      string
      Price             float64
      Currency          string
      ReturnUrl         string
      PaymentName       string
      PayerId           string    // 可选，用于 Airwallex 的 customer_id 字段
  }
  ```
- 返回参数：
  ```go
  type PayResp struct {
      PayUrl  string // 支付链接
      OrderId string // 订单ID
  }
  ```

#### 2.2 支付通知（Notify）
- 接口功能：处理支付状态回调
- 请求参数：
  - body []byte：通知内容
  - orderId string：订单ID
- 返回参数：
  ```go
  type NotifyResult struct {
      PaymentName        string
      PaymentStatus     string
      NotifyMessage     string
      ProductName       string
      ProductDisplayName string
      ProviderName      string
      Price            float64
      Currency         string
      OrderId          string
  }
  ```

### 3. 支付状态定义与映射
```go
const (
    PaymentStateCreated = "Created"  // 已创建
    PaymentStatePaid    = "Paid"     // 已支付
    PaymentStateTimeout = "Timeout"   // 超时
    PaymentStateError   = "Error"    // 错误
)

// Airwallex 状态映射
// SUCCEEDED -> PaymentStatePaid
// FAILED, CANCELLED -> PaymentStateError
// EXPIRED -> PaymentStateTimeout
// PENDING, REQUIRES_PAYMENT_METHOD, REQUIRES_CUSTOMER_ACTION -> PaymentStateCreated
```

### 4. 支付流程差异

#### Stripe 流程
1. 创建临时产品（Product）
2. 创建价格（Price）
3. 创建结账会话（Checkout Session）
4. 处理支付意向（Payment Intent）

#### Airwallex 流程
1. 获取访问令牌（getAccessToken）
2. 创建支付意向（Payment Intent）
3. 创建支付链接（Payment Link）
4. 处理支付回调

### 5. 发票接口
```go
GetInvoice(
    paymentName string,
    personName string,
    personIdCard string,
    personEmail string,
    personPhone string,
    invoiceType string,
    invoiceTitle string,
    invoiceTaxId string,
) (string, error)
```

### 6. 错误处理
```go
GetResponseError(err error) string
```
返回值：
- "success": 成功
- "fail": 失败

## Airwallex 特有实现

### 1. HTTP 客户端配置
```go
client := &http.Client{
    Timeout: 10 * time.Second,
}
```

### 2. API 认证
```go
// 获取访问令牌
func (pp *AirwallexPaymentProvider) getAccessToken() (string, error)

// 请求头设置
headers := {
    "x-client-id": pp.ClientId,
    "x-api-key": pp.APIKey,
    "Content-Type": "application/json"
}
```

### 3. 请求处理
```go
// 通用请求处理方法
func (pp *AirwallexPaymentProvider) doRequest(
    method string,
    url string,
    token string,
    body interface{},
) (map[string]interface{}, error)
```

## 测试要求
1. 环境变量配置：
   - AIRWALLEX_CLIENT_ID
   - AIRWALLEX_API_KEY
2. 测试用例应包含：
   - 支付创建测试
   - 支付状态回调测试
   - 错误处理测试
