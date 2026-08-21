package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/auth/googleid"
	"buk-yutnori/internal/httpapi"
	"buk-yutnori/internal/server"
	"buk-yutnori/internal/wsapi"
)

type config struct {
	googleClientID string
	listenAddr     string
	webRoot        string
}

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	verifier, err := googleid.New(config.googleClientID)
	if err != nil {
		return err
	}
	authService, err := auth.NewService(verifier, auth.NewMemoryStore(), rand.Reader, time.Now)
	if err != nil {
		return err
	}
	authHandler, err := httpapi.NewAuthHandler(authService, httpapi.Config{GoogleClientID: config.googleClientID})
	if err != nil {
		return err
	}
	prototypeRuntime, err := application.NewPrototypeRealtimeApplication(time.Now)
	if err != nil {
		return err
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if closeErr := prototypeRuntime.Close(closeContext); closeErr != nil {
			slog.Error("prototype realtime application shutdown failed", "error", closeErr)
		}
	}()
	realtimeSession, err := wsapi.NewRealtimeSession(prototypeRuntime.Processor(), prototypeRuntime.ChatEvents())
	if err != nil {
		return err
	}
	websocketHandler, err := wsapi.NewHandler(
		authService,
		realtimeSession,
		wsapi.DefaultConfig(httpapi.SessionCookieName),
	)
	if err != nil {
		return err
	}
	handler, err := server.NewHandler(authHandler, websocketHandler, config.webRoot)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              config.listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(deadline); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Warn("using in-memory authentication store; sessions do not survive restart")
	slog.Warn(
		"using in-memory prototype room and reconnect snapshot; state does not survive restart",
		"room_id", application.PrototypeRoomID,
		"match_id", application.PrototypeMatchID,
	)
	slog.Info("serving local prototype", "address", config.listenAddr, "web_root", config.webRoot)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

func loadConfig(getenv func(string) string) (config, error) {
	clientID := strings.TrimSpace(getenv("BUK_GOOGLE_CLIENT_ID"))
	if clientID == "" {
		return config{}, errors.New("BUK_GOOGLE_CLIENT_ID is required")
	}
	listenAddr := strings.TrimSpace(getenv("BUK_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = "127.0.0.1:8080"
	}
	webRoot := strings.TrimSpace(getenv("BUK_WEB_ROOT"))
	if webRoot == "" {
		webRoot = "build/client/web"
	}
	return config{googleClientID: clientID, listenAddr: listenAddr, webRoot: webRoot}, nil
}
