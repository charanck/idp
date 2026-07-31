// Example Go client for IDP: fetch every config/secret for a
// service+environment (decrypted into a map), and every feature flag
// (into a map of name -> enabled). Both use the same service client API key.
//
//	go mod init idp-example && go get github.com/fernet/fernet-go
//	IDP_API_KEY=... IDP_ENCRYPTION_KEY=... go run main.go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/fernet/fernet-go"
)

type configEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type featureFlag struct {
	Name      string `json:"name"`
	IsEnabled bool   `json:"is_enabled"`
}

// GetDecryptedConfigs fetches every config/secret for a service+environment
// via the S2S API and decrypts each value with this client's encryption key.
func GetDecryptedConfigs(baseURL, apiKey, encryptionKey, service, environment string) (map[string]string, error) {
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

// GetFeatureFlags fetches every feature flag for a service+environment into a
// map of name -> enabled.
func GetFeatureFlags(baseURL, apiKey, service, environment string) (map[string]bool, error) {
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list feature flags: unexpected status %d", resp.StatusCode)
	}

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

func main() {
	baseURL := os.Getenv("IDP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}

	apiKey := os.Getenv("IDP_API_KEY")

	configs, err := GetDecryptedConfigs(baseURL, apiKey, os.Getenv("IDP_ENCRYPTION_KEY"), "my-app", "prod")
	if err != nil {
		panic(err)
	}
	fmt.Println(configs["DB_PASSWORD"])

	flags, err := GetFeatureFlags(baseURL, apiKey, "my-app", "prod")
	if err != nil {
		panic(err)
	}
	if flags["NEW_CHECKOUT"] {
		fmt.Println("NEW_CHECKOUT is enabled")
	}
}
