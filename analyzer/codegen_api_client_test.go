package analyzer

import (
	"strings"
	"testing"
)

func TestGeneratedClientHasNoAuthenticationPreset(t *testing.T) {
	generated := string(generateClientFile())
	for _, forbidden := range []string{
		"localStorage",
		"access_token",
		"refresh_token",
		"/auth/refresh",
		"Authorization: Bearer",
		"credentials: \"include\"",
	} {
		if strings.Contains(generated, forbidden) {
			t.Errorf("generated client contains authentication preset %q", forbidden)
		}
	}
	if got := strings.Count(generated, `credentials: credentials ?? "omit"`); got != 3 {
		t.Errorf("default JSON, multipart, and blob fetchers must omit credentials; got %d defaults", got)
	}
	for _, expected := range []string{
		"headers?: HeadersInit",
		"credentials?: RequestCredentials",
		"export function setFetcher(fn: Fetcher)",
	} {
		if !strings.Contains(generated, expected) {
			t.Errorf("generated client is missing generic transport feature %q", expected)
		}
	}
}
