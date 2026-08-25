# In-app inbox

The pull-based, **persisted** counterpart to [Realtime events (SSE)](sse.md): `inapp`-channel
notifications are stored until the recipient fetches them, so nothing is lost if they weren't
connected when a notification was sent. The trade-off is it's pull, not push — poll it, or pair it
with SSE (use SSE to know *when* to refresh the inbox).

As with SSE, the caller is the end user, so this endpoint authenticates with a short-lived bearer
token, not `X-API-Key`.

## 1. Mint a session token

Same as [SSE, step 1](sse.md#1-mint-a-session-token):
`POST /api/v1/notifications/sessions` with `X-API-Key`, body `{"user_id": "user-123"}`, returns
`{"token": "...", "expires_in_seconds": 300}`.

## 2. Fetch and consume unread notifications

`GET /api/v1/notifications/inapp/unread`, authenticated with `Authorization: Bearer <token>`.

!!! warning "This call marks notifications as read"
    Every notification returned by this call is immediately marked read — there's no separate
    "peek without consuming" endpoint. If your client needs to display them again later, store
    the response; a second call won't return the same notifications.

```json title="Response"
[
  {
    "id": "d4e1...",
    "channel": "inapp",
    "recipient": { "user_id": "user-123" },
    "content": { "title": "Order shipped", "body": "Your order #4821 is on its way." },
    "status": "sent",
    "provider": "inapp",
    "provider_message_id": "inapp-...",
    "attempt": 1,
    "created_at": "Mon, 02 Jan 2006 15:04:05 GMT",
    "updated_at": "Mon, 02 Jan 2006 15:04:05 GMT"
  }
]
```

An empty array means there's nothing unread — not an error.

=== "cURL"

    ```bash
    TOKEN=$(curl -s -X POST "http://localhost:8000/api/v1/notifications/sessions" \
      -H "X-API-Key: <key_id>.<secret>" -H "Content-Type: application/json" \
      -d '{"user_id":"user-123"}' | jq -r .token)

    curl "http://localhost:8000/api/v1/notifications/inapp/unread" \
      -H "Authorization: Bearer $TOKEN"
    ```

=== "Python"

    ```python
    import requests

    def get_unread(base_url, token):
        response = requests.get(
            f"{base_url}/api/v1/notifications/inapp/unread",
            headers={"Authorization": f"Bearer {token}"},
            timeout=10,
        )
        response.raise_for_status()
        return response.json()

    unread = get_unread("http://localhost:8000", token)
    for n in unread:
        print(n["content"])
    ```

=== "Node.js / TypeScript"

    ```typescript
    async function getUnread(baseUrl: string, token: string) {
      const res = await fetch(`${baseUrl}/api/v1/notifications/inapp/unread`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!res.ok) {
        throw new Error(`Failed to fetch unread notifications: ${res.status}`);
      }
      return res.json();
    }

    const unread = await getUnread("http://localhost:8000", token);
    for (const n of unread) {
      console.log(n.content);
    }
    ```

=== "Go"

    ```go
    func getUnread(baseURL, token string) ([]byte, error) {
        req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/notifications/inapp/unread", nil)
        if err != nil {
            return nil, err
        }
        req.Header.Set("Authorization", "Bearer "+token)

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()
        return io.ReadAll(resp.Body)
    }
    ```

## Errors

| Status | Meaning |
|---|---|
| `401` | Missing/invalid/expired bearer token, or missing/invalid `X-API-Key` (from `sessions`). |
| `500` | Unexpected server error. |
