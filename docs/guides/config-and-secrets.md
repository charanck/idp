# Config & Secrets

Configs and secrets share one model (`ConfigEntry`) — `is_secret=true` is what makes a value a
secret. Both are created and managed through the web UI (**Applications → Environments →
Configs**); this guide covers reading them back programmatically as a service client.

## How it's encrypted

1. **Write** — an admin creates a config/secret via the web UI. The value is encrypted with
   `MASTER_ENCRYPTION_KEY` before it's stored; the UI never echoes the plaintext back.
2. **Read** — a service client calls `GET /api/v1/config/configs/list` with its `X-API-Key`. The
   server decrypts with the master key and **re-encrypts with that client's own `encryption_key`**
   (generated once, at client-creation time, shown only that one time) before returning it.
3. **Client-side decrypt** — the client decrypts locally with the key it was given at creation
   time. Non-secret configs are re-encrypted the same way for a uniform response shape, so the
   same decrypt step applies to every entry, not just ones with `is_secret: true`.

Losing `MASTER_ENCRYPTION_KEY` makes every stored value permanently unrecoverable. See
[Architecture: encryption flow](../architecture.md#encryption-flow) for the full picture.

## List configs/secrets

`GET /api/v1/config/configs/list?service=<name>&environment=<name>`, authenticated with
`X-API-Key: <key_id>.<secret>`.

Response is a JSON array:

```json
[
  {
    "id": "b3f1...",
    "service": "my-app",
    "environment": "prod",
    "key": "DATABASE_URL",
    "value": "gAAAAA...",
    "is_secret": true,
    "type": "string"
  }
]
```

`type` is one of `string`, `boolean`, `number`, `object`, `array` — the value's declared type
before encryption; `value` is always the Fernet-encrypted ciphertext string regardless of type.

=== "cURL"

    ```bash
    curl "http://localhost:8000/api/v1/config/configs/list?service=my-app&environment=prod" \
      -H "X-API-Key: <key_id>.<secret>"
    ```

=== "Python"

    ```python
    # pip install requests cryptography
    import requests
    from cryptography.fernet import Fernet

    def get_decrypted_configs(base_url, api_key, encryption_key, service, environment):
        response = requests.get(
            f"{base_url}/api/v1/config/configs/list",
            params={"service": service, "environment": environment},
            headers={"X-API-Key": api_key},
            timeout=10,
        )
        response.raise_for_status()

        fernet = Fernet(encryption_key.encode())
        return {
            entry["key"]: fernet.decrypt(entry["value"].encode()).decode()
            for entry in response.json()
        }

    configs = get_decrypted_configs(
        "http://localhost:8000", api_key, encryption_key, "my-app", "prod"
    )
    print(configs["DATABASE_URL"])
    ```

=== "Node.js / TypeScript"

    ```typescript
    // npm install fernet
    import Fernet from "fernet";

    interface ConfigResponse {
      key: string;
      value: string; // Fernet-encrypted with the calling client's encryption_key
      is_secret: boolean;
      type: "boolean" | "string" | "number" | "object" | "array";
    }

    async function getDecryptedConfigs(
      baseUrl: string,
      apiKey: string,        // "<key_id>.<secret>"
      encryptionKey: string, // this client's Fernet key, shown once at creation
      service: string,
      environment: string
    ): Promise<Record<string, string>> {
      const url = `${baseUrl}/api/v1/config/configs/list?service=${encodeURIComponent(
        service
      )}&environment=${encodeURIComponent(environment)}`;

      const res = await fetch(url, { headers: { "X-API-Key": apiKey } });
      if (!res.ok) {
        throw new Error(`Failed to list configs: ${res.status} ${await res.text()}`);
      }

      const configs: ConfigResponse[] = await res.json();
      const secret = new Fernet.Secret(encryptionKey);

      const result: Record<string, string> = {};
      for (const config of configs) {
        const token = new Fernet.Token({ secret, token: config.value, ttl: 0 });
        result[config.key] = token.decode() as string;
      }
      return result;
    }

    const configs = await getDecryptedConfigs(
      "http://localhost:8000", apiKey, encryptionKey, "my-app", "prod"
    );
    console.log(configs.DATABASE_URL);
    ```

=== "Go"

    ```go
    // go get github.com/fernet/fernet-go
    import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/url"

        "github.com/fernet/fernet-go"
    )

    type configEntry struct {
        Key   string `json:"key"`
        Value string `json:"value"`
    }

    func getDecryptedConfigs(baseURL, apiKey, encryptionKey, service, environment string) (map[string]string, error) {
        reqURL := fmt.Sprintf("%s/api/v1/config/configs/list?%s", baseURL, url.Values{
            "service":     {service},
            "environment": {environment},
        }.Encode())

        req, err := http.NewRequest(http.MethodGet, reqURL, nil)
        if err != nil {
            return nil, err
        }
        req.Header.Set("X-API-Key", apiKey)

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
            return nil, fmt.Errorf("list configs: unexpected status %d", resp.StatusCode)
        }

        var configs []configEntry
        if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
            return nil, err
        }

        keys := fernet.MustDecodeKeys(encryptionKey)
        result := make(map[string]string, len(configs))
        for _, c := range configs {
            decrypted := fernet.VerifyAndDecrypt([]byte(c.Value), 0, keys)
            if decrypted == nil {
                return nil, fmt.Errorf("failed to decrypt config %q", c.Key)
            }
            result[c.Key] = string(decrypted)
        }
        return result, nil
    }
    ```

## List configs/secrets (v2, key-value map)

`GET /api/v1/config/v2/configs/list?service=<name>&environment=<name>`, same auth and scope as v1
above. Additive, not a replacement: v1's array response is unchanged. Use v2 when the caller only
needs values and would otherwise discard `id`/`service`/`environment`/`type`/`is_secret` from every
v1 entry — the response is a flat map instead:

```json
{
  "DATABASE_URL": "gAAAAA...",
  "FEATURE_TIMEOUT_MS": "gAAAAA..."
}
```

Every value is still the Fernet ciphertext, encrypted with the calling client's own
`encryption_key` exactly as in v1 — decrypt each value the same way, there's just no per-entry
metadata to unwrap first. `type` and `is_secret` aren't in the v2 response, so if the caller needs
either of those, use v1 instead.

=== "cURL"

    ```bash
    curl "http://localhost:8000/api/v1/config/v2/configs/list?service=my-app&environment=prod" \
      -H "X-API-Key: <key_id>.<secret>"
    ```

=== "Python"

    ```python
    # pip install requests cryptography
    import requests
    from cryptography.fernet import Fernet

    def get_decrypted_configs_v2(base_url, api_key, encryption_key, service, environment):
        response = requests.get(
            f"{base_url}/api/v1/config/v2/configs/list",
            params={"service": service, "environment": environment},
            headers={"X-API-Key": api_key},
            timeout=10,
        )
        response.raise_for_status()

        fernet = Fernet(encryption_key.encode())
        return {
            key: fernet.decrypt(value.encode()).decode()
            for key, value in response.json().items()
        }

    configs = get_decrypted_configs_v2(
        "http://localhost:8000", api_key, encryption_key, "my-app", "prod"
    )
    print(configs["DATABASE_URL"])
    ```

=== "Node.js / TypeScript"

    ```typescript
    // npm install fernet
    import Fernet from "fernet";

    async function getDecryptedConfigsV2(
      baseUrl: string,
      apiKey: string,        // "<key_id>.<secret>"
      encryptionKey: string, // this client's Fernet key, shown once at creation
      service: string,
      environment: string
    ): Promise<Record<string, string>> {
      const url = `${baseUrl}/api/v1/config/v2/configs/list?service=${encodeURIComponent(
        service
      )}&environment=${encodeURIComponent(environment)}`;

      const res = await fetch(url, { headers: { "X-API-Key": apiKey } });
      if (!res.ok) {
        throw new Error(`Failed to list configs: ${res.status} ${await res.text()}`);
      }

      const encrypted: Record<string, string> = await res.json();
      const secret = new Fernet.Secret(encryptionKey);

      const result: Record<string, string> = {};
      for (const [key, value] of Object.entries(encrypted)) {
        const token = new Fernet.Token({ secret, token: value, ttl: 0 });
        result[key] = token.decode() as string;
      }
      return result;
    }

    const configs = await getDecryptedConfigsV2(
      "http://localhost:8000", apiKey, encryptionKey, "my-app", "prod"
    );
    console.log(configs.DATABASE_URL);
    ```

=== "Go"

    ```go
    // go get github.com/fernet/fernet-go
    import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/url"

        "github.com/fernet/fernet-go"
    )

    func getDecryptedConfigsV2(baseURL, apiKey, encryptionKey, service, environment string) (map[string]string, error) {
        reqURL := fmt.Sprintf("%s/api/v1/config/v2/configs/list?%s", baseURL, url.Values{
            "service":     {service},
            "environment": {environment},
        }.Encode())

        req, err := http.NewRequest(http.MethodGet, reqURL, nil)
        if err != nil {
            return nil, err
        }
        req.Header.Set("X-API-Key", apiKey)

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
            return nil, fmt.Errorf("list configs: unexpected status %d", resp.StatusCode)
        }

        var encrypted map[string]string
        if err := json.NewDecoder(resp.Body).Decode(&encrypted); err != nil {
            return nil, err
        }

        keys := fernet.MustDecodeKeys(encryptionKey)
        result := make(map[string]string, len(encrypted))
        for key, value := range encrypted {
            decrypted := fernet.VerifyAndDecrypt([]byte(value), 0, keys)
            if decrypted == nil {
                return nil, fmt.Errorf("failed to decrypt config %q", key)
            }
            result[key] = string(decrypted)
        }
        return result, nil
    }
    ```

## Config history and rollback

Every write is snapshotted as an immutable version. From a config's detail page in the web UI,
open its history to see prior versions (secret values are never shown — only that a version
changed, when, and by whom) and roll back to any of them; a rollback is recorded as a new version
rather than rewriting history. There is no S2S endpoint for history/rollback — it's web-UI-only.

## Errors

| Status | Meaning |
|---|---|
| `401` | Missing/invalid `X-API-Key`, or an inactive service client. |
| `429` | S2S rate limit exceeded (`S2S_AUTH_RATE_LIMIT`) — see [Configuration](../configuration.md#rate-limiting). Retry after the `Retry-After` header's value in seconds. |
| `500` | Unexpected server error. |

An unknown `service`/`environment` pair returns `200` with an empty array, not `404`.
