package main

import "testing"

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
