package password

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/session"
)

// PasswordHandler manages the HTTP routes for password authentication including
// registration, login, logout, password change, password reset, and email
// verification. Create one with [NewPasswordHandler] and register its routes
// with [PasswordHandler.RegisterRoutes].
type PasswordHandler struct {
	cfg     PasswordConfig
	store   *PasswordIdentityStore
	session *session.Manager
}

// NewPasswordHandler creates a new PasswordHandler with the given configuration,
// password identity store, and session manager.
func NewPasswordHandler(cfg PasswordConfig, store *PasswordIdentityStore, sessionManager *session.Manager) *PasswordHandler {
	return &PasswordHandler{
		cfg:     cfg.WithDefaults(),
		store:   store,
		session: sessionManager,
	}
}

// RegisterRoutes registers the password authentication routes on the given Chi
// router. All routes are mounted relative to the router (the caller should
// mount the router at [PasswordConfig.PathPrefix]).
//
// Routes:
//   - POST /register         — create a new account
//   - POST /login            — authenticate and create session
//   - POST /logout           — destroy session
//   - POST /change-password  — change password (requires current password)
//   - POST /forgot-password  — request a password reset
//   - POST /reset-password   — complete password reset with token
//   - POST /verify-email     — confirm email address with token
//   - POST /resend-verification — resend email verification
func (h *PasswordHandler) RegisterRoutes(r chi.Router) {
	r.Post("/register", h.RegisterHandler)
	r.Post("/login", h.LoginHandler)
	r.Post("/logout", h.LogoutHandler)
	r.Post("/change-password", h.ChangePasswordHandler)
	r.Post("/forgot-password", h.ForgotPasswordHandler)
	r.Post("/reset-password", h.ResetPasswordHandler)
	r.Post("/verify-email", h.VerifyEmailHandler)
	r.Post("/resend-verification", h.ResendVerificationHandler)
}

// --- Request/Response types ---

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
}

type registerResponse struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	EmailSent bool   `json:"email_verification_sent,omitempty"`
	Token     string `json:"token,omitempty"` // only in dev mode (no EmailSender)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	UserID                 string `json:"user_id"`
	TenantCount            int    `json:"tenant_count"`
	TenantSelectionRequired bool   `json:"tenant_selection_required"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type forgotPasswordResponse struct {
	Message string `json:"message"`
	Token   string `json:"token,omitempty"` // only in dev mode
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// --- Handlers ---

// RegisterHandler creates a new user account.
func (h *PasswordHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	userRef, err := h.store.Register(r.Context(), RegisterRequest{
		Email:    req.Email,
		Password: req.Password,
		Username: req.Username,
		Name:     req.Name,
	})
	if err != nil {
		handlePasswordError(w, err)
		return
	}

	resp := registerResponse{
		UserID: userRef.UserID.String(),
		Email:  req.Email,
	}

	// If email verification is required and no EmailSender, generate a token
	if h.cfg.RequireEmailVerification && h.cfg.EmailSender == nil {
		token, tokenErr := h.store.InitiateEmailVerification(r.Context(), userRef.UserID)
		if tokenErr == nil {
			resp.Token = token
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// LoginHandler authenticates a user and creates a session.
func (h *PasswordHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	ipAddress := r.Header.Get("X-Real-IP")
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
	}

	userRef, err := h.store.Authenticate(r.Context(), req.Email, req.Password, ipAddress)
	if err != nil {
		handlePasswordError(w, err)
		return
	}

	// List memberships to determine tenant selection
	memberships, err := h.store.ListMemberships(r.Context(), userRef.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list memberships")
		return
	}

	// Build session claims (same structure as OIDC)
	claims := auth.SessionClaims{
		Authenticated: true,
		UserID:        userRef.UserID,
		Issuer:        userRef.Issuer,
		Subject:       userRef.Subject,
	}

	if len(memberships) == 1 {
		claims.ActiveTenantID = &memberships[0].TenantID
	}

	h.session.Put(r.Context(), h.cfg.SessionKey, claims)

	resp := loginResponse{
		UserID:      userRef.UserID.String(),
		TenantCount: len(memberships),
	}
	if len(memberships) > 1 {
		resp.TenantSelectionRequired = true
	}

	writeJSON(w, http.StatusOK, resp)
}

// LogoutHandler destroys the current session.
func (h *PasswordHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.getSessionClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	// Clear the session
	h.session.Put(r.Context(), h.cfg.SessionKey, auth.SessionClaims{
		UserID:  claims.UserID,
		Issuer:  claims.Issuer,
		Subject: claims.Subject,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ChangePasswordHandler changes the authenticated user's password.
func (h *PasswordHandler) ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.getSessionClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	if err := h.store.ChangePassword(r.Context(), claims.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		handlePasswordError(w, err)
		return
	}

	// Notify via email if sender is configured
	if h.cfg.EmailSender != nil {
		_ = h.cfg.EmailSender.SendPasswordChanged(r.Context(), claims.Subject)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password changed successfully"})
}

// ForgotPasswordHandler initiates a password reset. Always returns 202 Accepted
// to prevent user enumeration, regardless of whether the email exists.
func (h *PasswordHandler) ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	token, _ := h.store.InitiatePasswordReset(r.Context(), req.Email)

	resp := forgotPasswordResponse{
		Message: "If an account exists with this email, a password reset link has been sent.",
	}

	// Dev mode: return the token in the response
	if token != "" && h.cfg.EmailSender == nil {
		resp.Token = token
	} else if token != "" && h.cfg.EmailSender != nil {
		_ = h.cfg.EmailSender.SendPasswordReset(r.Context(), req.Email, token)
	}

	writeJSON(w, http.StatusAccepted, resp)
}

// ResetPasswordHandler completes a password reset using a token.
func (h *PasswordHandler) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	if err := h.store.CompletePasswordReset(r.Context(), req.Token, req.NewPassword); err != nil {
		handlePasswordError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password has been reset successfully"})
}

// VerifyEmailHandler confirms an email address using a verification token.
func (h *PasswordHandler) VerifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to parse request body")
		return
	}

	if err := h.store.VerifyEmail(r.Context(), req.Token); err != nil {
		handlePasswordError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// ResendVerificationHandler resends an email verification token. Requires
// authentication.
func (h *PasswordHandler) ResendVerificationHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.getSessionClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	token, err := h.store.InitiateEmailVerification(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate verification token")
		return
	}

	if h.cfg.EmailSender != nil {
		_ = h.cfg.EmailSender.SendEmailVerification(r.Context(), claims.Subject, token)
		writeJSON(w, http.StatusOK, map[string]string{"message": "verification email sent"})
		return
	}

	// Dev mode: return the token
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "verification token generated",
		"token":   token,
	})
}

// --- Helpers ---

func (h *PasswordHandler) getSessionClaims(r *http.Request) (auth.SessionClaims, bool) {
	claims, ok := h.session.Get(r.Context(), h.cfg.SessionKey).(auth.SessionClaims)
	if !ok || !claims.Authenticated {
		return auth.SessionClaims{}, false
	}
	return claims, true
}

func handlePasswordError(w http.ResponseWriter, err error) {
	var pwErr *PasswordError
	if errors.As(err, &pwErr) {
		writeError(w, pwErr.Status, errorString(pwErr.Err), pwErr.Detail)
		return
	}
	// Fallback for unexpected errors
	writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
}

func errorString(err error) string {
	// Return a short error code based on the sentinel error
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return "invalid_credentials"
	case errors.Is(err, ErrAccountLocked):
		return "account_locked"
	case errors.Is(err, ErrPasswordPolicyViolation):
		return "password_policy_violation"
	case errors.Is(err, ErrEmailAlreadyRegistered):
		return "email_already_registered"
	case errors.Is(err, ErrEmailNotVerified):
		return "email_not_verified"
	case errors.Is(err, ErrTokenExpired), errors.Is(err, ErrTokenConsumed), errors.Is(err, ErrTokenInvalid):
		return "invalid_token"
	case errors.Is(err, ErrUserNotFound):
		return "user_not_found"
	default:
		return "internal_error"
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{
		Error:   code,
		Message: message,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// Ensure session claims are registered for gob encoding.
func init() {
	// Register SessionClaims with gob so scs can serialize/deserialize it.
	// This is idempotent if auth package already registered it.
	session.RegisterGobTypes(auth.SessionClaims{})
}

