package testoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type TestUser struct {
	Subject           string
	Email             string
	EmailVerified     bool
	Name              string
	Nickname          string
	PreferredUsername string
}

type TestUserBuilder struct {
	user TestUser
}

func NewTestUser(prefix string) *TestUserBuilder {
	suffix := uuid.New().String()[:8]
	return &TestUserBuilder{
		user: TestUser{
			Subject:           prefix + "-" + suffix,
			Email:             prefix + "-" + suffix + "@example.com",
			EmailVerified:     true,
			PreferredUsername: prefix + "-" + suffix,
		},
	}
}

func (b *TestUserBuilder) WithName(name string) *TestUserBuilder {
	b.user.Name = name
	return b
}

func (b *TestUserBuilder) Build() *TestUser {
	return &b.user
}

type MockOIDCProvider struct {
	server      *httptest.Server
	privateKey  *rsa.PrivateKey
	kid         string
	clientID    string
	codeToUser  map[string]*TestUser
	tokenToUser map[string]*TestUser
	mu          sync.Mutex
}

func NewMockOIDCProvider(clientID string) *MockOIDCProvider {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("testoidc: failed to generate RSA key: " + err.Error())
	}

	kid := "test-key-" + uuid.New().String()[:8]

	m := &MockOIDCProvider{
		privateKey:  privateKey,
		kid:         kid,
		clientID:    clientID,
		codeToUser:  make(map[string]*TestUser),
		tokenToUser: make(map[string]*TestUser),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/token", m.handleToken)
	mux.HandleFunc("/userinfo", m.handleUserInfo)
	mux.HandleFunc("/keys", m.handleJWKS)

	m.server = httptest.NewServer(mux)
	return m
}

func (m *MockOIDCProvider) IssuerURL() string {
	return m.server.URL
}

func (m *MockOIDCProvider) Close() {
	m.server.Close()
}

func (m *MockOIDCProvider) CreateAuthCode(user *TestUser) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	code := "auth-code-" + uuid.New().String()
	m.codeToUser[code] = user
	return code
}

func (m *MockOIDCProvider) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	issuer := m.server.URL
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/auth",
		"token_endpoint":                        issuer + "/token",
		"userinfo_endpoint":                     issuer + "/userinfo",
		"jwks_uri":                              issuer + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
	})
}

func (m *MockOIDCProvider) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	user, ok := m.codeToUser[code]
	if ok {
		delete(m.codeToUser, code)
	}
	m.mu.Unlock()

	if !ok {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}

	now := time.Now()
	claims := map[string]any{
		"sub": user.Subject,
		"iss": m.server.URL,
		"aud": m.clientID,
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
	}

	idToken, err := m.signJWT(claims)
	if err != nil {
		http.Error(w, "token signing failed", http.StatusInternalServerError)
		return
	}

	accessToken := "access-" + uuid.New().String()

	m.mu.Lock()
	m.tokenToUser[accessToken] = user
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

func (m *MockOIDCProvider) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accessToken := strings.TrimPrefix(authHeader, "Bearer ")

	m.mu.Lock()
	user, ok := m.tokenToUser[accessToken]
	m.mu.Unlock()

	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sub":                user.Subject,
		"email":              user.Email,
		"email_verified":     user.EmailVerified,
		"name":               user.Name,
		"nickname":           user.Nickname,
		"preferred_username": user.PreferredUsername,
	})
}

func (m *MockOIDCProvider) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pubKey := &m.privateKey.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pubKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pubKey.E)).Bytes())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": m.kid,
				"use": "sig",
				"alg": "RS256",
				"n":   n,
				"e":   e,
			},
		},
	})
}

func (m *MockOIDCProvider) signJWT(claims map[string]any) (string, error) {
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": m.kid,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB64, nil
}
