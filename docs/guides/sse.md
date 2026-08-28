# Realtime events (SSE)

Streams delivery notices to a specific end user in real time over Server-Sent Events. This is
**fire-and-forget** — nothing is replayed for events published while the client wasn't connected.
If you need a durable, catch-up-able inbox instead, see [In-app inbox](inapp-inbox.md).

The caller here is the *end user* the notifications belong to, not the service client — so this
endpoint doesn't accept `X-API-Key`. Instead, your backend (holding the service-client API key)
mints a short-lived bearer token on the user's behalf and hands it to whatever is opening the
stream (a browser tab, a mobile app, etc.).

Only `inapp` sends ever publish an SSE event — `email`, `sms`, and `whatsapp` are fire-and-forget
with no live push, since there's no "the recipient is watching a stream" concept for those
channels. Poll [In-app inbox](inapp-inbox.md) or your own delivery records if you need status for
those.

## 1. Mint a session token

`POST /api/v1/notifications/sessions`, authenticated with `X-API-Key: <key_id>.<secret>`.

```json title="Request body"
{ "user_id": "user-123" }
```

```json title="Response"
{ "token": "eyJ...", "expires_in_seconds": 300 }
```

The token is a short-lived, Fernet-signed credential scoped to that one `user_id` — it authorizes
the bearer to read *that user's* events/inbox and nothing else.

## 2. Open the stream

`GET /api/v1/notifications/sse/events`, authenticated with `Authorization: Bearer <token>` (the
token from step 1 — **not** the service-client API key).

Each event arrives as a standard SSE `data:` line:

```text
data: {"id":"d4e1...","channel":"inapp","status":"sent",...}

```

=== "cURL"

    ```bash
    TOKEN=$(curl -s -X POST "http://localhost:8000/api/v1/notifications/sessions" \
      -H "X-API-Key: <key_id>.<secret>" -H "Content-Type: application/json" \
      -d '{"user_id":"user-123"}' | jq -r .token)

    curl -N "http://localhost:8000/api/v1/notifications/sse/events" \
      -H "Authorization: Bearer $TOKEN"
    ```

=== "Python"

    ```python
    # pip install requests
    import json
    import requests

    def mint_session(base_url, api_key, user_id):
        response = requests.post(
            f"{base_url}/api/v1/notifications/sessions",
            json={"user_id": user_id},
            headers={"X-API-Key": api_key},
            timeout=10,
        )
        response.raise_for_status()
        return response.json()["token"]

    def stream_events(base_url, token):
        with requests.get(
            f"{base_url}/api/v1/notifications/sse/events",
            headers={"Authorization": f"Bearer {token}"},
            stream=True,
            timeout=None,
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines(decode_unicode=True):
                if line and line.startswith("data: "):
                    event = json.loads(line[len("data: "):])
                    print(event)

    token = mint_session("http://localhost:8000", api_key, "user-123")
    stream_events("http://localhost:8000", token)
    ```

=== "Node.js / TypeScript (browser)"

    In a browser, `EventSource` can't set custom headers, so pass the token as a query
    parameter isn't supported by this API — instead, use `fetch` with a `ReadableStream`, or a
    library like [`@microsoft/fetch-event-source`](https://github.com/Azure/fetch-event-source)
    that supports headers:

    ```typescript
    import { fetchEventSource } from "@microsoft/fetch-event-source";

    async function mintSession(baseUrl: string, apiKey: string, userId: string): Promise<string> {
      const res = await fetch(`${baseUrl}/api/v1/notifications/sessions`, {
        method: "POST",
        headers: { "X-API-Key": apiKey, "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: userId }),
      });
      const { token } = await res.json();
      return token;
    }

    const token = await mintSession("http://localhost:8000", apiKey, "user-123");

    await fetchEventSource("http://localhost:8000/api/v1/notifications/sse/events", {
      headers: { Authorization: `Bearer ${token}` },
      onmessage(msg) {
        console.log(JSON.parse(msg.data));
      },
    });
    ```

=== "Go"

    ```go
    func streamEvents(baseURL, token string) error {
        req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/notifications/sse/events", nil)
        if err != nil {
            return err
        }
        req.Header.Set("Authorization", "Bearer "+token)

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return err
        }
        defer resp.Body.Close()

        scanner := bufio.NewScanner(resp.Body)
        for scanner.Scan() {
            line := scanner.Text()
            if data, ok := strings.CutPrefix(line, "data: "); ok {
                fmt.Println("event:", data)
            }
        }
        return scanner.Err()
    }
    ```

## Errors

| Status | Meaning |
|---|---|
| `401` | Missing/invalid/expired bearer token (from `sse/events`), or missing/invalid `X-API-Key` (from `sessions`). |
| `400` | Missing `user_id` when minting a session. |

The connection stays open until the client disconnects or the request context is cancelled (e.g.
server shutdown) — there's no server-initiated timeout beyond that.
