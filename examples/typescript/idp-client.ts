/**
 * Example TypeScript client for IDP: fetch every config/secret for a
 * service+environment (decrypted into an object), and every feature flag
 * (into an object of name -> enabled). Both use the same service client API key.
 *
 *   npm install fernet
 *   IDP_API_KEY=... IDP_ENCRYPTION_KEY=... npx tsx idp-client.ts
 *
 * Requires Node 18+ (global fetch).
 */
import Fernet from "fernet";

interface ConfigResponse {
  id: string;
  service: string;
  environment: string;
  key: string;
  value: string; // Fernet-encrypted with the calling client's encryption_key
  is_secret: boolean;
  type: "boolean" | "string" | "number" | "object" | "array";
}

interface FeatureFlagResponse {
  id: string;
  service: string;
  environment: string;
  name: string;
  description: string;
  is_enabled: boolean;
}

/**
 * Fetches every config/secret for a service+environment via the S2S API,
 * decrypts each value with this client's encryption_key, and returns them
 * as a plain { KEY: value } object.
 */
export async function getDecryptedConfigs(
  baseUrl: string,
  apiKey: string,        // "<key_id>.<secret>" from POST /api/v1/auth/s2s/clients
  encryptionKey: string, // this client's Fernet key, from the same response
  service: string,
  environment: string
): Promise<Record<string, string>> {
  const url = `${baseUrl}/api/v1/config/configs/list?service=${encodeURIComponent(
    service
  )}&environment=${encodeURIComponent(environment)}`;

  const res = await fetch(url, {
    headers: { "X-API-Key": apiKey },
  });
  if (!res.ok) {
    throw new Error(`Failed to list configs: ${res.status} ${await res.text()}`);
  }

  const configs: ConfigResponse[] = await res.json();
  const secret = new Fernet.Secret(encryptionKey);

  const result: Record<string, string> = {};
  for (const config of configs) {
    const token = new Fernet.Token({ secret, token: config.value, ttl: 0 });
    result[config.key] = token.decode();
  }
  return result;
}

/**
 * Fetches every feature flag for a service+environment and returns them as
 * a plain { FLAG_NAME: enabled } object. Flags aren't encrypted.
 */
export async function getFeatureFlags(
  baseUrl: string,
  apiKey: string,
  service: string,
  environment: string
): Promise<Record<string, boolean>> {
  const url = `${baseUrl}/api/v1/config/feature-flags?service=${encodeURIComponent(
    service
  )}&environment=${encodeURIComponent(environment)}`;

  const res = await fetch(url, {
    headers: { "X-API-Key": apiKey },
  });
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

async function main() {
  const baseUrl = process.env.IDP_BASE_URL || "http://localhost:8000";
  const apiKey = process.env.IDP_API_KEY!;

  const configs = await getDecryptedConfigs(
    baseUrl,
    apiKey,
    process.env.IDP_ENCRYPTION_KEY!,
    "my-app",
    "prod"
  );
  console.log(configs.DB_PASSWORD);

  const flags = await getFeatureFlags(baseUrl, apiKey, "my-app", "prod");
  if (flags.NEW_CHECKOUT) {
    console.log("NEW_CHECKOUT is enabled");
  }
}

if (require.main === module) {
  main();
}
