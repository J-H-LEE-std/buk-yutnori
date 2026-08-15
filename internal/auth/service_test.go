package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginVerifiesGoogleIdentityAndStoresOnlySessionDigest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	random := append(bytes.Repeat([]byte{0x11}, userIDRandomBytes), bytes.Repeat([]byte{0x22}, sessionTokenRandomBytes)...)
	verifier := &stubVerifier{identity: GoogleIdentity{Subject: "google-sub-123"}}
	store := &recordingStore{}
	service, err := NewService(verifier, store, bytes.NewReader(random), func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Login(context.Background(), "signed-google-id-token")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if verifier.credential != "signed-google-id-token" {
		t.Fatalf("verifier credential = %q", verifier.credential)
	}
	if store.subject != "google-sub-123" {
		t.Fatalf("stored Google subject = %q", store.subject)
	}
	if store.proposedUserID == "" || string(store.proposedUserID) == string(store.subject) {
		t.Fatalf("proposed internal user ID = %q", store.proposedUserID)
	}
	if result.User.ID != store.proposedUserID {
		t.Fatalf("result user ID = %q, want %q", result.User.ID, store.proposedUserID)
	}
	if result.Token == "" || result.Token == "signed-google-id-token" {
		t.Fatalf("issued session token = %q", result.Token)
	}
	wantDigest := sha256.Sum256([]byte(result.Token))
	if store.session.Digest != wantDigest {
		t.Fatalf("stored digest = %x, want %x", store.session.Digest, wantDigest)
	}
	if store.session.CreatedAt != now || store.session.LastUsedAt != now {
		t.Fatalf("stored session timestamps = %+v", store.session)
	}
	if want := now.Add(30 * 24 * time.Hour); store.session.ExpiresAt != want || result.ExpiresAt != want {
		t.Fatalf("expiry = %v / %v, want %v", store.session.ExpiresAt, result.ExpiresAt, want)
	}
}

func TestLoginRejectsInvalidCredentialBeforeGeneratingOrStoringSession(t *testing.T) {
	t.Parallel()

	verifier := &stubVerifier{err: ErrInvalidCredential}
	store := &recordingStore{}
	service, err := NewService(verifier, store, &failReader{}, time.Now)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Login(context.Background(), "bad-token")
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredential", err)
	}
	if store.issueCalls != 0 {
		t.Fatalf("IssueSession() calls = %d, want 0", store.issueCalls)
	}
}

func TestLoginRejectsMissingGoogleSubject(t *testing.T) {
	t.Parallel()

	service, err := NewService(
		&stubVerifier{identity: GoogleIdentity{}},
		&recordingStore{},
		bytes.NewReader(make([]byte, userIDRandomBytes+sessionTokenRandomBytes)),
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Login(context.Background(), "token-without-subject")
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredential", err)
	}
}

func TestAuthenticateAndLogoutHashRawCookieBeforeCallingStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	store := &recordingStore{useUser: User{ID: testUserID}}
	service, err := NewService(&stubVerifier{}, store, bytes.NewReader(nil), func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	wantDigest := sha256.Sum256([]byte("browser-cookie-token"))

	user, err := service.Authenticate(context.Background(), "browser-cookie-token")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if user.ID != testUserID || store.usedDigest != wantDigest || store.usedAt != now {
		t.Fatalf("authentication result = %+v, digest = %x, usedAt = %v", user, store.usedDigest, store.usedAt)
	}

	if err := service.Logout(context.Background(), "browser-cookie-token"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if store.revokedDigest != wantDigest || store.revokedAt != now {
		t.Fatalf("revoked digest = %x, revokedAt = %v", store.revokedDigest, store.revokedAt)
	}
}

func TestAuthenticateAndLogoutRejectEmptyRawToken(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	service, err := NewService(&stubVerifier{}, store, bytes.NewReader(nil), time.Now)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Authenticate(context.Background(), ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
	}
	if err := service.Logout(context.Background(), ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Logout() error = %v, want ErrUnauthenticated", err)
	}
	if store.useCalls != 0 || store.revokeCalls != 0 {
		t.Fatalf("store calls = use %d, revoke %d", store.useCalls, store.revokeCalls)
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	validVerifier := &stubVerifier{}
	validStore := &recordingStore{}
	validRandom := bytes.NewReader(nil)

	tests := []struct {
		name     string
		verifier IdentityVerifier
		store    Store
		random   io.Reader
		clock    func() time.Time
	}{
		{name: "verifier", store: validStore, random: validRandom, clock: time.Now},
		{name: "store", verifier: validVerifier, random: validRandom, clock: time.Now},
		{name: "random", verifier: validVerifier, store: validStore, clock: time.Now},
		{name: "clock", verifier: validVerifier, store: validStore, random: validRandom},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewService(test.verifier, test.store, test.random, test.clock); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewService() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestServiceSerializesConcurrentUseOfInjectedRandomSource(t *testing.T) {
	t.Parallel()

	random := &concurrencyDetectingReader{}
	service, err := NewService(fixedVerifier{}, NewMemoryStore(), random, time.Now)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	const loginCount = 16
	errorsChannel := make(chan error, loginCount)
	var waitGroup sync.WaitGroup
	for range loginCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := service.Login(context.Background(), "credential")
			errorsChannel <- err
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Login() error = %v", err)
		}
	}
}

type stubVerifier struct {
	identity   GoogleIdentity
	err        error
	credential string
}

const testUserID UserID = "usr_EREREREREREREREREREREQ"

func (v *stubVerifier) Verify(_ context.Context, credential string) (GoogleIdentity, error) {
	v.credential = credential
	return v.identity, v.err
}

type recordingStore struct {
	issueCalls     int
	subject        GoogleSubject
	proposedUserID UserID
	session        NewSession
	issueErr       error
	useCalls       int
	usedDigest     SessionDigest
	usedAt         time.Time
	useUser        User
	useErr         error
	revokeCalls    int
	revokedDigest  SessionDigest
	revokedAt      time.Time
	revokeErr      error
}

func (s *recordingStore) IssueSession(_ context.Context, subject GoogleSubject, proposedUserID UserID, session NewSession) (User, error) {
	s.issueCalls++
	s.subject = subject
	s.proposedUserID = proposedUserID
	s.session = session
	return User{ID: proposedUserID}, s.issueErr
}

func (s *recordingStore) UseSession(_ context.Context, digest SessionDigest, usedAt time.Time) (User, error) {
	s.useCalls++
	s.usedDigest = digest
	s.usedAt = usedAt
	return s.useUser, s.useErr
}

func (s *recordingStore) RevokeSession(_ context.Context, digest SessionDigest, revokedAt time.Time) error {
	s.revokeCalls++
	s.revokedDigest = digest
	s.revokedAt = revokedAt
	return s.revokeErr
}

type failReader struct{}

func (*failReader) Read([]byte) (int, error) {
	return 0, errors.New("random source must not be used")
}

type fixedVerifier struct{}

func (fixedVerifier) Verify(context.Context, string) (GoogleIdentity, error) {
	return GoogleIdentity{Subject: "concurrent-google-subject"}, nil
}

type concurrencyDetectingReader struct {
	reading atomic.Bool
	value   atomic.Uint32
}

func (r *concurrencyDetectingReader) Read(buffer []byte) (int, error) {
	if !r.reading.CompareAndSwap(false, true) {
		return 0, errors.New("concurrent random read")
	}
	defer r.reading.Store(false)
	time.Sleep(time.Millisecond)
	value := byte(r.value.Add(1))
	for index := range buffer {
		buffer[index] = value
	}
	return len(buffer), nil
}
