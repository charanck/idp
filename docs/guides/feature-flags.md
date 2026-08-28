# Feature Flags

Feature flags are scoped per application + environment, same as configs, and are created/toggled
through the web UI (**Feature Flags**). This guide covers reading them back programmatically.

## List flags

`GET /api/v1/config/feature-flags?service=<name>&environment=<name>`, authenticated with
`X-API-Key: <key_id>.<secret>`. Flags aren't encrypted — the response is plain JSON.

```json
[
  {
    "id": "9c2e...",
    "service": "my-app",
    "environment": "prod",
    "name": "NEW_CHECKOUT",
    "description": "Roll out the redesigned checkout flow",
    "is_enabled": true,
    "created_count": 1
  }
]
```

#### cURL

```bash
curl "http://localhost:8000/api/v1/config/feature-flags?service=my-app&environment=prod" \
  -H "X-API-Key: <key_id>.<secret>"
```

#### Python

```python
import requests

def get_feature_flags(base_url, api_key, service, environment):
    response = requests.get(
        f"{base_url}/api/v1/config/feature-flags",
        params={"service": service, "environment": environment},
        headers={"X-API-Key": api_key},
        timeout=10,
    )
    response.raise_for_status()
    return {flag["name"]: flag["is_enabled"] for flag in response.json()}

flags = get_feature_flags("http://localhost:8000", api_key, "my-app", "prod")
if flags.get("NEW_CHECKOUT"):
    print("NEW_CHECKOUT is enabled")
```

#### Node.js / TypeScript

```typescript
interface FeatureFlagResponse {
  name: string;
  is_enabled: boolean;
}

async function getFeatureFlags(
  baseUrl: string,
  apiKey: string,
  service: string,
  environment: string
): Promise<Record<string, boolean>> {
  const url = `${baseUrl}/api/v1/config/feature-flags?service=${encodeURIComponent(
    service
  )}&environment=${encodeURIComponent(environment)}`;

  const res = await fetch(url, { headers: { "X-API-Key": apiKey } });
  if (!res.ok) {
    throw new Error(`Failed to list feature flags: ${res.status} ${await res.text()}`);
  }

  const flags: FeatureFlagResponse[] = await res.json();
  const result: Record<string, boolean> = {};
  for (const flag of flags) {
    result[flag.name] = flag.is_enabled;
  }
  return result;
}

const flags = await getFeatureFlags("http://localhost:8000", apiKey, "my-app", "prod");
if (flags.NEW_CHECKOUT) {
  console.log("NEW_CHECKOUT is enabled");
}
```

#### Go

```go
type featureFlag struct {
    Name      string `json:"name"`
    IsEnabled bool   `json:"is_enabled"`
}

func getFeatureFlags(baseURL, apiKey, service, environment string) (map[string]bool, error) {
    reqURL := fmt.Sprintf("%s/api/v1/config/feature-flags?%s", baseURL, url.Values{
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

    var flags []featureFlag
    if err := json.NewDecoder(resp.Body).Decode(&flags); err != nil {
        return nil, err
    }

    result := make(map[string]bool, len(flags))
    for _, f := range flags {
        result[f.Name] = f.IsEnabled
    }
    return result, nil
}
```

## Errors

Same as [Config & Secrets](config-and-secrets.md#errors) — `401` for a bad/missing API key,
`429` if S2S-rate-limited, `200` with an empty array for an unknown `service`/`environment` pair.
