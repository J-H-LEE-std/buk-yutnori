package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/auth/googleid"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/httpapi"
	"buk-yutnori/internal/profile"
	"buk-yutnori/internal/server"
	"buk-yutnori/internal/storage"
	"buk-yutnori/internal/wsapi"
)

type config struct {
	googleClientID string
	listenAddr     string
	webRoot        string
	dbPath         string
}

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfigFromSources(os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	verifier, err := googleid.New(config.googleClientID)
	if err != nil {
		return err
	}
	eventStore, err := storage.OpenSQLite(config.dbPath)
	if err != nil {
		return fmt.Errorf("open canonical event store: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			slog.Error("event store shutdown failed", "error", closeErr)
		}
	}()
	// One canonical SQLite handle implements distinct storage boundaries. Keep
	// the profile-facing use explicitly typed so room snapshots cannot depend on
	// the event-store interface alone.
	var profileStore profile.Store = eventStore
	authService, err := auth.NewService(verifier, eventStore, rand.Reader, time.Now)
	if err != nil {
		return err
	}
	authHandler, err := httpapi.NewAuthHandler(authService, httpapi.Config{GoogleClientID: config.googleClientID})
	if err != nil {
		return err
	}
	profileHandler, err := httpapi.NewProfileHandler(authService, profileStore)
	if err != nil {
		return err
	}
	roomsRegistry, err := application.NewRoomRegistry(time.Now)
	if err != nil {
		return err
	}
	boardGraph, err := board.LoadFile("spec/board_graph.yaml")
	if err != nil {
		return fmt.Errorf("load canonical board graph: %w", err)
	}
	if err := roomsRegistry.AttachBoardGraph(boardGraph); err != nil {
		return err
	}
	if err := roomsRegistry.AttachEventStore(eventStore); err != nil {
		return err
	}
	if err := roomsRegistry.AttachProfileStore(profileStore); err != nil {
		return err
	}
	roomsHandler, err := httpapi.NewRoomsHandler(authService, roomsRegistry)
	if err != nil {
		return err
	}
	realtimeRuntime, err := application.NewRealtimeApplicationWithProfiles(time.Now, roomsRegistry, eventStore, profileStore)
	if err != nil {
		return err
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if closeErr := realtimeRuntime.Close(closeContext); closeErr != nil {
			slog.Error("realtime application shutdown failed", "error", closeErr)
		}
	}()
	realtimeSession, err := wsapi.NewRealtimeSession(realtimeRuntime.Processor(), realtimeRuntime.LobbyChatEvents())
	if err != nil {
		return err
	}
	if err := realtimeSession.SetLobbyEvents(realtimeRuntime.Lobbies()); err != nil {
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
	handler, err := server.NewHandler(authHandler, profileHandler, roomsHandler, websocketHandler, config.webRoot)
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
	sessionCleanupContext, cancelSessionCleanup := context.WithCancel(shutdownContext)
	sessionCleanupTicker := time.NewTicker(sessionCleanupInterval)
	sessionCleanupDone := make(chan struct{})
	go func() {
		defer close(sessionCleanupDone)
		runExpiredSessionCleanup(
			sessionCleanupContext,
			eventStore,
			time.Now,
			sessionCleanupTicker.C,
			sessionCleanupBatchSize,
			func(err error) { slog.Error("expired session cleanup failed", "error", err) },
		)
	}()
	defer func() {
		stopExpiredSessionCleanup(cancelSessionCleanup, sessionCleanupTicker, sessionCleanupDone)
	}()
	go func() {
		<-shutdownContext.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(deadline); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Warn(
		"using in-memory room registry and match runtime; state does not survive restart",
		"lobby_chat_room_id", application.LobbyChatRoomID,
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
	dbPath := strings.TrimSpace(getenv("BUK_DB_PATH"))
	if dbPath == "" {
		dbPath = "buk.db"
	}
	return config{googleClientID: clientID, listenAddr: listenAddr, webRoot: webRoot, dbPath: dbPath}, nil
}

// loadConfigFromSources gives an explicitly supplied environment value
// precedence over repository-local google.yaml. The local file is only a
// public browser-test convenience and is never a credential store.
func loadConfigFromSources(getenv func(string) string, readFile func(string) ([]byte, error)) (config, error) {
	if getenv == nil || readFile == nil {
		return config{}, errors.New("configuration sources are required")
	}
	if strings.TrimSpace(getenv("BUK_GOOGLE_CLIENT_ID")) != "" {
		return loadConfig(getenv)
	}

	data, err := readFile("google.yaml")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return loadConfig(getenv)
		}
		return config{}, fmt.Errorf("read local google.yaml: %w", err)
	}
	var local struct {
		WebClientID string `yaml:"web_client_id"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&local); err != nil {
		return config{}, fmt.Errorf("decode local google.yaml: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return config{}, fmt.Errorf("decode trailing local google.yaml: %w", err)
		}
		return config{}, errors.New("local google.yaml must contain one document")
	}
	clientID := strings.TrimSpace(local.WebClientID)
	if clientID == "" {
		return config{}, errors.New("local google.yaml requires web_client_id")
	}
	return loadConfig(func(key string) string {
		if key == "BUK_GOOGLE_CLIENT_ID" {
			return clientID
		}
		return getenv(key)
	})
}
