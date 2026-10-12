package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lalmax-pro/lalmax-nvr/internal/config"
	"github.com/stretchr/testify/require"
)

func TestEncryptionKeyWebSettings(t *testing.T) {
	t.Setenv("NVR_ENCRYPTION_KEY", "")
	t.Setenv("NVR_ENCRYPTION_KEY_FILE", "")
	config.SetWebEncryptionKeyFile("")
	t.Cleanup(func() { config.SetWebEncryptionKeyFile("") })

	configPath := filepath.Join(t.TempDir(), "lalmax-nvr.yaml")
	h := NewHandler(nil, nil, func(next http.Handler) http.Handler { return next }, nil, nil, configPath, nil, nil)
	get := httptest.NewRecorder()
	h.Routes().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/settings/encryption-key", nil))
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), `"enabled":false`)
	require.Contains(t, get.Body.String(), `"managed_by":"none"`)

	key := bytes.Repeat([]byte{0x4a}, 32)
	keyValue := base64.StdEncoding.EncodeToString(key)
	body, err := json.Marshal(map[string]string{"key": keyValue})
	require.NoError(t, err)

	put := httptest.NewRecorder()
	h.Routes().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/settings/encryption-key", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.NotContains(t, put.Body.String(), keyValue)

	keyPath := configPath + ".encryption-key"
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.Equal(t, key, config.GetEncryptionKey())
	require.Equal(t, "web", config.EncryptionKeySource())

	ciphertext, err := config.Encrypt("emby-access-token", config.GetEncryptionKey())
	require.NoError(t, err)
	config.SetWebEncryptionKeyFile(keyPath) // Simulate process restart and reload from disk.
	decrypted, err := config.Decrypt(ciphertext, config.GetEncryptionKey())
	require.NoError(t, err)
	require.Equal(t, "emby-access-token", decrypted)

	secondPut := httptest.NewRecorder()
	h.Routes().ServeHTTP(secondPut, httptest.NewRequest(http.MethodPut, "/api/settings/encryption-key", bytes.NewReader(body)))
	require.Equal(t, http.StatusConflict, secondPut.Code)
}

func TestEncryptionKeyWebSettingsRejectsInvalidKey(t *testing.T) {
	t.Setenv("NVR_ENCRYPTION_KEY", "")
	t.Setenv("NVR_ENCRYPTION_KEY_FILE", "")
	config.SetWebEncryptionKeyFile("")
	t.Cleanup(func() { config.SetWebEncryptionKeyFile("") })

	h := NewHandler(nil, nil, func(next http.Handler) http.Handler { return next }, nil, nil, filepath.Join(t.TempDir(), "lalmax-nvr.yaml"), nil, nil)
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/settings/encryption-key", bytes.NewBufferString(`{"key":"too-short"}`)))
	require.Equal(t, http.StatusBadRequest, rr.Code)
	_, err := os.Stat(h.configPath + ".encryption-key")
	require.ErrorIs(t, err, os.ErrNotExist)
}
