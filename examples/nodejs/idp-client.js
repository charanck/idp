/**
 * Example Node.js client for IDP: fetch every config/secret for a
 * service+environment (decrypted into an object), and every feature flag
 * (into an object of name -> enabled). Both use the same service client API key.
 *
 *   npm install fernet
 *   IDP_API_KEY=... IDP_ENCRYPTION_KEY=... node idp-client.js
 *
 * Requires Node 18+ (global fetch). On older Node, install and require
 * "node-fetch" instead.
 */
const Fernet = require("fernet");

/** Fetch every config/secret for a service+environment and decrypt it into an object. */
async function getDecryptedConfigs(baseUrl, apiKey, encryptionKey, service, environment) {
  const url = `${baseUrl}/api/v1/config/configs/list?service=${encodeURIComponent(
    service
  )}&environment=${encodeURIComponent(environment)}`;

  const res = await fetch(url, { headers: { "X-API-Key": apiKey } });
  if (!res.ok) {
    throw new Error(`Failed to list configs: ${res.status} ${await res.text()}`);
  }

  const configs = await res.json();
  const secret = new Fernet.Secret(encryptionKey);

  const result = {};
  for (const config of configs) {
    const token = new Fernet.Token({ secret, token: config.value, ttl: 0 });
    result[config.key] = token.decode();
  }
  return result;
}

/** Fetch every feature flag for a service+environment into an object of name -> enabled. */
async function getFeatureFlags(baseUrl, apiKey, service, environment) {
  const url = `${baseUrl}/api/v1/config/feature-flags?service=${encodeURIComponent(
    service
  )}&environment=${encodeURIComponent(environment)}`;

  const res = await fetch(url, { headers: { "X-API-Key": apiKey } });
  if (!res.ok) {
    throw new Error(`Failed to list feature flags: ${res.status} ${await res.text()}`);
  }

  const flags = await res.json();
  const result = {};
  for (const flag of flags) {
    result[flag.name] = flag.is_enabled;
  }
  return result;
}

module.exports = { getDecryptedConfigs, getFeatureFlags };

if (require.main === module) {
  (async () => {
    const baseUrl = process.env.IDP_BASE_URL || "http://localhost:8000";
    const apiKey = process.env.IDP_API_KEY;

    const configs = await getDecryptedConfigs(
      baseUrl,
      apiKey,
      process.env.IDP_ENCRYPTION_KEY,
      "my-app",
      "prod"
    );
    console.log(configs.DB_PASSWORD);

    const flags = await getFeatureFlags(baseUrl, apiKey, "my-app", "prod");
    if (flags.NEW_CHECKOUT) {
      console.log("NEW_CHECKOUT is enabled");
    }
  })();
}
