package main

import (
	"errors"
	"io/fs"
	"testing"
)

func TestLoadConfigRequiresGoogleClientIDAndUsesLocalDefaults(t *testing.T) {
	t.Parallel()

	if _, err := loadConfig(func(string) string { return "" }); err == nil {
		t.Fatal("loadConfig() without Google client ID error = nil")
	}

	config, err := loadConfig(func(key string) string {
		if key == "BUK_GOOGLE_CLIENT_ID" {
			return "web-client-id.apps.googleusercontent.com"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.listenAddr != "127.0.0.1:8080" || config.webRoot != "build/client/web" {
		t.Fatalf("config defaults = %+v", config)
	}
}

func TestLoadConfigAcceptsExplicitAddressAndWebRoot(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"BUK_GOOGLE_CLIENT_ID": "client-id",
		"BUK_LISTEN_ADDR":      "localhost:9090",
		"BUK_WEB_ROOT":         "/srv/client",
	}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.googleClientID != "client-id" || config.listenAddr != "localhost:9090" || config.webRoot != "/srv/client" {
		t.Fatalf("config = %+v", config)
	}
}

func TestLoadConfigFromSourcesUsesLocalPublicClientIDOnlyWhenEnvironmentIsUnset(t *testing.T) {
	t.Parallel()

	config, err := loadConfigFromSources(func(string) string { return "" }, func(path string) ([]byte, error) {
		if path != "google.yaml" {
			t.Fatalf("path = %q", path)
		}
		return []byte("web_client_id: local-client.apps.googleusercontent.com\n"), nil
	})
	if err != nil {
		t.Fatalf("loadConfigFromSources() error = %v", err)
	}
	if config.googleClientID != "local-client.apps.googleusercontent.com" {
		t.Fatalf("googleClientID = %q", config.googleClientID)
	}

	config, err = loadConfigFromSources(func(key string) string {
		if key == "BUK_GOOGLE_CLIENT_ID" {
			return "environment-client.apps.googleusercontent.com"
		}
		return ""
	}, func(string) ([]byte, error) {
		t.Fatal("local file must not be read when environment is explicit")
		return nil, nil
	})
	if err != nil || config.googleClientID != "environment-client.apps.googleusercontent.com" {
		t.Fatalf("environment precedence config=%+v error=%v", config, err)
	}
}

func TestLoadConfigFromSourcesRejectsMissingOrCredentialLikeLocalFile(t *testing.T) {
	t.Parallel()

	if _, err := loadConfigFromSources(func(string) string { return "" }, func(string) ([]byte, error) {
		return nil, fs.ErrNotExist
	}); err == nil {
		t.Fatal("missing local file without environment error = nil")
	}
	if _, err := loadConfigFromSources(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("web_client_id: public\nclient_secret: forbidden\n"), nil
	}); err == nil {
		t.Fatal("credential-like local file error = nil")
	}
	if _, err := loadConfigFromSources(func(string) string { return "" }, func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}); err == nil {
		t.Fatal("unreadable local file error = nil")
	}
}
