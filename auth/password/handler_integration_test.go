package password

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/session"
)

func setupHandlerTest(t *testing.T) (*chi.Mux, string) {
	t.Helper()
	db := requireIntegrationDB(t)

	inner, err := auth.NewPostgresIdentityStore(db.Pool, auth.PostgresStoreConfig{
		DefaultTenantName: "Handler Test " + t.Name(),
	})
	require.NoError(t, err)

	cfg := PasswordConfig{
		Enabled: true,
		Hashing: HashingConfig{
			Memory:      4096,
			Iterations:  1,
			Parallelism: 1,
			SaltLength:  8,
			KeyLength:   16,
		},
	}

	store, err := NewPasswordIdentityStore(db.Pool, inner, cfg, nil)
	require.NoError(t, err)

	sessMgr := session.NewMemory(session.Config{
		Lifetime: 24 * time.Hour,
	})
	session.RegisterGobTypes(auth.SessionClaims{})

	handler := NewPasswordHandler(cfg, store, sessMgr)

	r := chi.NewRouter()
	r.Use(sessMgr.LoadAndSave)
	handler.RegisterRoutes(r)

	return r, uniqueEmail(t)
}

func doJSON(t *testing.T, router *chi.Mux, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(w.Body).Decode(v))
}

func TestHandler_Register_Success(t *testing.T) {
	router, email := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: "secure-password-123",
	})

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp registerResponse
	decodeResponse(t, w, &resp)
	assert.NotEmpty(t, resp.UserID)
	assert.Equal(t, email, resp.Email)
}

func TestHandler_Register_MissingFields(t *testing.T) {
	router, _ := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/register", registerRequest{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Register_DuplicateEmail(t *testing.T) {
	router, email := setupHandlerTest(t)

	// First registration succeeds
	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: "first-password-123",
	})
	require.Equal(t, http.StatusCreated, w.Code)

	// Second registration with the same email returns 409
	w = doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: "second-password-456",
	})
	assert.Equal(t, http.StatusConflict, w.Code)

	var errResp errorResponse
	decodeResponse(t, w, &errResp)
	assert.Equal(t, "email_already_registered", errResp.Error)
}

func TestHandler_Login_Success(t *testing.T) {
	router, email := setupHandlerTest(t)
	password := "test-password-123"

	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: password,
	})
	require.Equal(t, http.StatusCreated, w.Code)

	w = doJSON(t, router, "POST", "/login", loginRequest{
		Email:    email,
		Password: password,
	})
	assert.Equal(t, http.StatusOK, w.Code)

	var resp loginResponse
	decodeResponse(t, w, &resp)
	assert.NotEmpty(t, resp.UserID)
	assert.Equal(t, 1, resp.TenantCount)
	assert.False(t, resp.TenantSelectionRequired)

	cookies := w.Result().Cookies()
	assert.NotEmpty(t, cookies, "session cookie should be set after login")
}

func TestHandler_Login_InvalidCredentials(t *testing.T) {
	router, _ := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/login", loginRequest{
		Email:    "nonexistent@example.com",
		Password: "wrong-password",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_ForgotPassword_AlwaysReturns202(t *testing.T) {
	router, _ := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/forgot-password", forgotPasswordRequest{
		Email: "nonexistent@example.com",
	})
	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp forgotPasswordResponse
	decodeResponse(t, w, &resp)
	assert.Contains(t, resp.Message, "If an account exists")
}

func TestHandler_ForgotPassword_ExistingEmail_ReturnsToken(t *testing.T) {
	router, email := setupHandlerTest(t)

	// Register
	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: "test-password-123",
	})
	require.Equal(t, http.StatusCreated, w.Code)

	// Forgot password
	w = doJSON(t, router, "POST", "/forgot-password", forgotPasswordRequest{Email: email})
	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp forgotPasswordResponse
	decodeResponse(t, w, &resp)
	assert.NotEmpty(t, resp.Token, "dev mode should return token in response")
}

func TestHandler_ResetPassword_Success(t *testing.T) {
	router, email := setupHandlerTest(t)
	oldPassword := "old-password-123"
	newPassword := "new-password-456"

	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: oldPassword,
	})
	require.Equal(t, http.StatusCreated, w.Code)

	// Forgot password to get token
	w = doJSON(t, router, "POST", "/forgot-password", forgotPasswordRequest{Email: email})
	require.Equal(t, http.StatusAccepted, w.Code)
	var forgotResp forgotPasswordResponse
	decodeResponse(t, w, &forgotResp)
	require.NotEmpty(t, forgotResp.Token)

	// Reset password
	w = doJSON(t, router, "POST", "/reset-password", resetPasswordRequest{
		Token:       forgotResp.Token,
		NewPassword: newPassword,
	})
	assert.Equal(t, http.StatusOK, w.Code)

	// Old password should fail
	w = doJSON(t, router, "POST", "/login", loginRequest{
		Email:    email,
		Password: oldPassword,
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// New password should work
	w = doJSON(t, router, "POST", "/login", loginRequest{
		Email:    email,
		Password: newPassword,
	})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_VerifyEmail_Success(t *testing.T) {
	router, email := setupHandlerTest(t)
	db := requireIntegrationDB(t)

	w := doJSON(t, router, "POST", "/register", registerRequest{
		Email:    email,
		Password: "test-password-123",
	})
	require.Equal(t, http.StatusCreated, w.Code)
	var regResp registerResponse
	decodeResponse(t, w, &regResp)

	// Get a verification token via the store
	ctx := t.Context()
	inner, err := auth.NewPostgresIdentityStore(db.Pool, auth.PostgresStoreConfig{
		DefaultTenantName: "Verify Tenant",
	})
	require.NoError(t, err)
	store, err := NewPasswordIdentityStore(db.Pool, inner, PasswordConfig{Enabled: true}, nil)
	require.NoError(t, err)

	userID, err := uuid.Parse(regResp.UserID)
	require.NoError(t, err)

	token, err := store.InitiateEmailVerification(ctx, userID)
	require.NoError(t, err)

	// Verify email
	w = doJSON(t, router, "POST", "/verify-email", verifyEmailRequest{Token: token})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_ChangePassword_Unauthorized(t *testing.T) {
	router, _ := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/change-password", changePasswordRequest{
		CurrentPassword: "old",
		NewPassword:     "new",
	})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_SecurityHeaders(t *testing.T) {
	router, _ := setupHandlerTest(t)

	w := doJSON(t, router, "POST", "/register", registerRequest{})
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache", w.Header().Get("Pragma"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
}
