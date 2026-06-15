# Spec: API Layer — OpenAI Compatible Endpoint

## POST /v1/chat/completions

### Scenario: Happy path — basic fusion request

Given OpenFusion is running with `budget` preset configured
When I send:
```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer test-key

{
  "model": "openfusion/budget",
  "messages": [
    {"role": "user", "content": "比较 Ridge 和 Lasso 回归的异同"}
  ]
}
```
Then response status is `200 OK`
And response body is a valid OpenAI chat.completion JSON

### Scenario: Unauthorized access

Given OpenFusion is running with `auth_token: "secret123"` in config
When I send a POST without Authorization header
Then response status is `401 Unauthorized`
And body contains `{"error": "unauthorized"}`

### Scenario: Unknown model

Given OpenFusion is running without `openfusion/nonexistent` preset
When I send request with `model: "openfusion/nonexistent"`
Then response status is `400 Bad Request`
And body contains `{"error": "unknown model: openfusion/nonexistent"}`

### Scenario: Empty messages

Given OpenFusion is running
When I send request with `messages: []`
Then response status is `400 Bad Request`
And body contains `{"error": "messages array is empty"}`

## GET /v1/models

### Scenario: List available presets

Given OpenFusion is running with 3 configured presets
When I send `GET /v1/models`
Then response status is `200`
And body contains array with all preset names and descriptions
