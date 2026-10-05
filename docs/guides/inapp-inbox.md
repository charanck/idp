# In-app inbox

The pull-based, **persisted** counterpart to [Realtime events (SSE)](sse.md): `inapp`-channel
notifications are stored until the recipient fetches them, so nothing is lost if they weren't
connected when a notification was sent. The trade-off is it's pull, not push — poll it, or pair it
with SSE (use SSE to know *when* to refresh the inbox).

As with SSE, the caller is the end user, so this endpoint authenticates with a short-lived bearer
token, not `X-API-Key`.

## 1. Mint a session token

Same as [SSE, step 1](sse.md#1-mint-a-session-token):
`POST /api/v1/notifications/sessions` with `X-API-Key`, body
`{"user_id": "user-123", "service": "orders"}`, returns
`{"token": "...", "expires_in_seconds": 300}`.

## 2. List in-app notifications without marking them as read

`GET /api/v1/notifications/inapp`, authenticated with `Authorization: ******`

This endpoint lists the caller's in-app notifications without modifying their read state. It is the non-destructive view for an inbox or dashboard that should not mark messages as read just because they were fetched.

## 3. Fetch unread notifications

`GET /api/v1/notifications/inapp/unread`, authenticated with `Authorization: ******`

This endpoint lists the caller's unread in-app notifications without marking them as read. It is useful for fetching new notifications without consuming them.

Response:

```json
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

## 4. Mark notifications as read

`POST /api/v1/notifications/inapp/mark-as-read`, authenticated with `Authorization: ******`

This endpoint marks one or more notifications as read.

Request:

```json
{
  "notification_ids": ["d4e1...", "d4e2..."]
}
```

Response:

```json
{
  "status": "success"
}
```

## Errors

| Status | Meaning |
|---|---|
| `400` | Invalid request body or missing/invalid notification IDs. |
| `401` | Missing/invalid/expired bearer token, or missing/invalid `X-API-Key` (from `sessions`). |
| `500` | Unexpected server error. |
