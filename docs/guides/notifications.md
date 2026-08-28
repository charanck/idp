# Notifications

The notification API queues messages across four channels — `email`, `sms`, `whatsapp`, `inapp` —
and a background worker (running inside the same `cmd/server` process) delivers them with
retries. All endpoints on this page are authenticated with `X-API-Key: <key_id>.<secret>`, the
same service-client key used for configs/flags.

!!! note "SMS and WhatsApp are simulated"
    The `sms` and `whatsapp` channels ship as skeleton providers: they validate input, log the
    send, and report success without calling a real provider — there's no Twilio/etc. integration
    wired up yet. `email` sends real mail over SMTP (configured under **Notification Settings** in
    the web UI); `inapp` is fully functional, since "delivery" is just persisting the row for the
    recipient to pull later (see [In-app inbox](inapp-inbox.md)).

## Create a notification

`POST /api/v1/notifications`

```json title="Request body"
{
  "channel": "inapp",
  "recipient": { "user_id": "user-123" },
  "content": { "title": "Order shipped", "body": "Your order #4821 is on its way." },
  "idempotency_key": "order-4821-shipped"
}
```

`recipient` and `content` are raw JSON whose shape depends on `channel` — each channel validates
its own schema:

| Channel | `recipient` requires | `content` requires |
|---|---|---|
| `email` | `email` (string); `user_id` optional | `subject`; `body` optional |
| `sms` | `phone` (string); `user_id` optional | `body` |
| `whatsapp` | `phone` (string); `user_id` optional | `body` |
| `inapp` | `user_id` (string, **required** — it's how the notification is ever retrieved) | `title`; `body` optional |

`idempotency_key` is optional — reusing one for a notification still in `queued` status re-enqueues
delivery instead of creating a duplicate; once a notification reaches a terminal status
(`sent`/`failed`), reusing its key returns that existing notification unchanged.

```json title="201 Response"
{
  "id": "d4e1...",
  "channel": "inapp",
  "recipient": { "user_id": "user-123" },
  "content": { "title": "Order shipped", "body": "Your order #4821 is on its way." },
  "status": "queued",
  "attempt": 0,
  "idempotency_key": "order-4821-shipped",
  "created_at": "Mon, 02 Jan 2006 15:04:05 GMT",
  "updated_at": "Mon, 02 Jan 2006 15:04:05 GMT"
}
```

`status` transitions `queued` → `processing` → `sent` or `failed` (with retries via `processing` →
`queued` in between, up to the worker's retry limit). `provider` / `provider_message_id` are set
once a channel's `Send` succeeds; `error` is set on the most recent failed attempt.

=== "cURL"

    ```bash
    curl -X POST "http://localhost:8000/api/v1/notifications" \
      -H "X-API-Key: <key_id>.<secret>" \
      -H "Content-Type: application/json" \
      -d '{
            "channel": "inapp",
            "recipient": {"user_id": "user-123"},
            "content": {"title": "Order shipped", "body": "Your order #4821 is on its way."}
          }'
    ```

=== "Python"

    ```python
    import requests

    def create_notification(base_url, api_key, channel, recipient, content, idempotency_key=None):
        body = {"channel": channel, "recipient": recipient, "content": content}
        if idempotency_key:
            body["idempotency_key"] = idempotency_key
        response = requests.post(
            f"{base_url}/api/v1/notifications",
            json=body,
            headers={"X-API-Key": api_key},
            timeout=10,
        )
        response.raise_for_status()
        return response.json()

    notification = create_notification(
        "http://localhost:8000", api_key, "inapp",
        {"user_id": "user-123"},
        {"title": "Order shipped", "body": "Your order #4821 is on its way."},
    )
    print(notification["id"], notification["status"])
    ```

=== "Node.js / TypeScript"

    ```typescript
    async function createNotification(
      baseUrl: string,
      apiKey: string,
      channel: "email" | "sms" | "whatsapp" | "inapp",
      recipient: Record<string, unknown>,
      content: Record<string, unknown>,
      idempotencyKey?: string
    ) {
      const res = await fetch(`${baseUrl}/api/v1/notifications`, {
        method: "POST",
        headers: { "X-API-Key": apiKey, "Content-Type": "application/json" },
        body: JSON.stringify({ channel, recipient, content, idempotency_key: idempotencyKey }),
      });
      if (!res.ok) {
        throw new Error(`Failed to create notification: ${res.status} ${await res.text()}`);
      }
      return res.json();
    }

    const notification = await createNotification(
      "http://localhost:8000", apiKey, "inapp",
      { user_id: "user-123" },
      { title: "Order shipped", body: "Your order #4821 is on its way." }
    );
    console.log(notification.id, notification.status);
    ```

=== "Go"

    ```go
    type createRequest struct {
        Channel        string          `json:"channel"`
        Recipient      json.RawMessage `json:"recipient"`
        Content        json.RawMessage `json:"content"`
        IdempotencyKey string          `json:"idempotency_key,omitempty"`
    }

    func createNotification(baseURL, apiKey string, req createRequest) ([]byte, error) {
        body, err := json.Marshal(req)
        if err != nil {
            return nil, err
        }
        httpReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/notifications", bytes.NewReader(body))
        if err != nil {
            return nil, err
        }
        httpReq.Header.Set("X-API-Key", apiKey)
        httpReq.Header.Set("Content-Type", "application/json")

        resp, err := http.DefaultClient.Do(httpReq)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()
        return io.ReadAll(resp.Body)
    }
    ```

## List notifications

`GET /api/v1/notifications?channel=<channel>&status=<status>` — both query params are optional
filters. Returns a JSON array of the same shape as the create response.

```bash
curl "http://localhost:8000/api/v1/notifications?channel=inapp&status=sent" \
  -H "X-API-Key: <key_id>.<secret>"
```

## Get a single notification

`GET /api/v1/notifications/:id` — useful for polling a just-created notification until it leaves
`queued`/`processing`:

```bash
curl "http://localhost:8000/api/v1/notifications/d4e1..." \
  -H "X-API-Key: <key_id>.<secret>"
```

Returns `404` if `id` doesn't exist, `400` if it isn't a valid UUID.

## Getting notifications to the end user

Once a notification is `sent`, the `inapp` and (indirectly, via `inapp`) real-time channels are
how the *end user* — not the service client — retrieves it:

- [Realtime events (SSE)](sse.md) — push, fire-and-forget, not persisted.
- [In-app inbox](inapp-inbox.md) — pull-based, persisted, mark-as-read on fetch.

Both require minting a short-lived, user-scoped bearer token first via `POST
/api/v1/notifications/sessions` — see either guide for the full flow.

## Errors

| Status | Meaning |
|---|---|
| `400` | Invalid JSON body; unknown `channel`; recipient/content fails that channel's schema; malformed `id`. |
| `401` | Missing/invalid `X-API-Key`. |
| `404` | Notification `id` not found (`GET /:id` only). |
| `429` | S2S rate limit exceeded. |
| `500` | Unexpected server error. |
