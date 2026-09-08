package main

// This harness exists only in the test binary. It never reads google.yaml or
// the developer database, and production builds cannot enable its verifier.
import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"buk-yutnori/internal/application"
	"buk-yutnori/internal/auth"
	"buk-yutnori/internal/domain/board"
	"buk-yutnori/internal/httpapi"
	"buk-yutnori/internal/server"
	"buk-yutnori/internal/storage"
	"buk-yutnori/internal/wsapi"
)

type browserVerifier struct{}

func (browserVerifier) Verify(_ context.Context, credential string) (auth.GoogleIdentity, error) {
	if !strings.HasPrefix(credential, "browser-test-player-") || len(credential) > 100 {
		return auth.GoogleIdentity{}, errors.New("invalid test identity")
	}
	return auth.GoogleIdentity{Subject: auth.GoogleSubject(credential)}, nil
}

func TestBrowserHarness(t *testing.T) {
	if os.Getenv("BUK_BROWSER_HARNESS") != "1" {
		t.Skip("explicit browser harness only")
	}
	store, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "browser.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := auth.NewService(browserVerifier{}, store, rand.Reader, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	authHandler, err := httpapi.NewAuthHandler(service, httpapi.Config{GoogleClientID: "browser-test.apps.googleusercontent.com"})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := httpapi.NewProfileHandler(service, store)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := application.NewRoomRegistry(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := board.LoadFile("../../spec/board_graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.AttachBoardGraph(graph); err != nil {
		t.Fatal(err)
	}
	if err = registry.AttachEventStore(store); err != nil {
		t.Fatal(err)
	}
	if err = registry.AttachProfileStore(store); err != nil {
		t.Fatal(err)
	}
	rooms, err := httpapi.NewRoomsHandler(service, registry)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := application.NewRealtimeApplicationWithProfiles(time.Now, registry, store, store)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	session, err := wsapi.NewRealtimeSession(runtime.Processor(), runtime.LobbyChatEvents())
	if err != nil {
		t.Fatal(err)
	}
	if err = session.SetLobbyEvents(runtime.Lobbies()); err != nil {
		t.Fatal(err)
	}
	if err = session.SetPresence(runtime.Lobbies()); err != nil {
		t.Fatal(err)
	}
	websocket, err := wsapi.NewHandler(service, session, wsapi.DefaultConfig(httpapi.SessionCookieName))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.NewHandler(authHandler, profiles, rooms, websocket, "../../build/client/web")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Addr: "127.0.0.1:8766", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer httpServer.Close()
	deadline := time.AfterFunc(10*time.Minute, func() { _ = httpServer.Close() })
	defer deadline.Stop()
	t.Log("BROWSER_HARNESS http://localhost:8766 (isolated temporary SQLite)")
	if err = httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}
