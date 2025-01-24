# AirWallex APIs

# Sandbox

测试卡：https://www.airwallex.com/docs/payments__test-card-numbers

## API权限

1. **获取访问令牌** [Obtain access token](https://www.airwallex.com/docs/api?v=2024-08-07#/Authentication/API_Access/)
通过请求标头中指定 x-client-id 和 x-api-key 获取访问令牌。
返回的令牌是调用所有其他 API 端点所必需的，该 HTTP 标头必须包含 Authorization: Bearer [token]。
有效期 30 分钟，可多次用于所有其他 API 端点，直至过期。
建议依靠 expires_at 来获取准确的令牌过期时间。
此 API 强制执行每分钟 100 个请求的速率限制。如果请求数量超过此限制，API 将响应错误代码 429。

```sh
POST /api/v1/authentication/login
Header
x-client-id AIRWALLEX_CLIENT_ID
x-api-key AIRWALLEX_API_KEY
return { token, expires_at }
```

## 支付意愿


```sh
[id] a unique payment intent id, eg: "int_e65tkXCSzJrsMpTrzoFrjaau53"
[request_id] a unique request id, eg: "9d6e4dc5-cf79-48a6-8fab-f629dc8764db"
```

1. **创建支付意愿** [Create a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents_create/post)
通过创建 PaymentIntent 开始从客户处收取付款。必须提供付款金额和货币以及商家订单ID(merchant_order_id)

```sh
POST /api/v1/pa/payment_intents/create
JSON Body:
{
  "amount": 0.01,
  "currency": "USD",
  "customer_id": "cus_ps8e0ZgQzd2QnCxVpzJrHD6KOVu",
  "return_url": "http://localhost/return",
  "merchant_order_id": "test123",
  "descriptor": "Test|test123|awx",
  "request_id": [request_id]
}
```

3. **更新支付意愿** [Update a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id__update/post)
某些属性仅允许在 REQUIRES_PAYMENT_METHOD 或 REQUIRES_CUSTOMER_ACTION 状态下更新。根据您更新的属性和 PaymentIntent 的状态，您可能需要再次确认 PaymentIntent。例如，在 REQUIRES_CUSTOMER_ACTION 状态下更新货币和金额始终需要您再次确认 PaymentIntent。更新行为遵循以下原则：
  a. 请求负载中省略的字段将被忽略，并且不会对现有值产生影响。
  b. 请求负载中提供的字段将合并到现有数据中（数组字段除外，它将是完全替换）。
  c. 可以通过传递空值或空值（仅适用于字符串类型）来使条件字段无效。

```sh
POST /api/v1/pa/payment_intents/[id]/update
JSON Body:
{
  "request_id": [request_id]
}
```

4. **确认支付意愿** [Confirm a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id__confirm/post)
当您的客户准备按照 PaymentIntent 中的详细信息付款时，请调用此端点。
至少，必须将 PaymentMethod 设置为付款的资金来源。这可以通过在确认 PaymentIntent 时提供 payment_method 对象来完成。

```sh
POST /api/v1/pa/payment_intents/[id]/confirm
JSON Body:
{
  "request_id": [request_id]
}
```

5. **继续认支付意愿** [Continue to confirm a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id__continue/post)
在某些情况下，例如 3DS(3D Secure，增加在线支付的安全性，防止欺诈交易) 和 DCC(Dynamic Currency Conversion，动态货币转换，允许持卡人用本国货币支付)，客户需要在初始确认请求后多次提供附加信息。3DS 负责安全性，而 DCC 负责货币转换。

```sh
POST /api/v1/pa/payment_intents/[id]/confirm_continue
JSON Body:
{
  "request_id": [request_id]
}
```

6. **捕获支付意愿** [Capture a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id__capture/post)
捕获已确认但尚未捕获或仅部分捕获的资金。如果 auto_capture=false 且其 next_action 指示需要捕获，则 PaymentIntent 是可捕获的。

```sh
POST /api/v1/pa/payment_intents/[id]/capture
JSON Body:
{
  "request_id": [request_id]
}
```

7. **取消支付意愿** [Cancel a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id__cancel/post)
当 PaymentIntent 处于以下状态之一时，可以取消它：REQUIRES_PAYMENT_METHOD、REQUIRES_CUSTOMER_ACTION、REQUIRES_CAPTURE。一旦取消，所有未结清的未捕获资金都将退还。

```sh
POST /api/v1/pa/payment_intents/[id]/cancel
JSON Body:
{
  "request_id": [request_id]
}
```

8. **查询支付意愿** [Get list of PaymentIntents](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents/get)

```sh
GET /api/v1/pa/payment_intents[id]
```
  


2. **获取支付意愿** [Retrieve a PaymentIntent](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Intents/_api_v1_pa_payment_intents__id_/get)

```sh
GET /api/v1/pa/payment_intents/[id]
```

## 收单链接（@airwallex/components-sdk）

代码中有个redirectToCheckout，生成如下链接：
[https://checkout.airwallex.com/#/standalone/checkout?intent_id=[intent_id]&client_secret=[client_secret]&currency=[currency]&country_code=JPY&from=http://localhost:3000&sessionId=4a8a225e-94d5-45b7-9370-c8f81d5da7d1]()

`sessionId` 约等于`OrderId`，casdoor里是`r.PaymentName`


```sh
失败回调：
FailURL: http://localhost/return?error=default_backend_error&id=int_sgpdpllrch3ykrmtx6f&type=FAIL_URL
```


## 收款链接

1. **创建支付链接** [Create a PaymentLink](https://www.airwallex.com/docs/api?v=2024-08-07#/Payment_Acceptance/Payment_Links/_api_v1_pa_payment_links_create/post)
允许您使用付款链接 URL 接受购物者的付款。付款链接 URL 将您的购物者带到 Airwallex 托管的安全结账页面，他们可以在此使用自己喜欢的付款方式付款。
PaymentLink 支持两种定价选项：
固定定价 - 付款金额和货币均已锁定。购物者必须以给定的货币支付准确的金额。
灵活定价 - 购物者从您指定的支持货币列表中选择付款，并决定要支付的确切金额。


```sh
POST /api/v1/pa/payment_links/create
JSON Body:
{
  "payment_intent_id": "int_e65tkXCSzJrsMpTrzoFrjaau53",
  "title": "Order #123",
  "reusable": false
}
```

