package media

import (
	"strings"
	"testing"
)

func TestSanitizeHLSPullErrorRedactsURLSecrets(t *testing.T) {
	got := sanitizeHLSPullError(`Get "https://user:pass@example.test/live/index.m3u8?token=secret&key=value": context deadline exceeded`)
	if strings.Contains(got, "user") || strings.Contains(got, "pass") || strings.Contains(got, "secret") || strings.Contains(got, "key=value") {
		t.Fatalf("sanitized pull error leaked URL credentials or query parameters: %s", got)
	}
	if !strings.Contains(got, "example.test") || !strings.Contains(got, "context deadline exceeded") || !strings.Contains(got, "%5Bpath%20redacted%5D") {
		t.Fatalf("sanitized pull error lost useful diagnostic context: %s", got)
	}
}
