package auth

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuth_RefreshAndRevoke(t *testing.T) {
	userStore := NewMemoryUserStore()
	sessionStore := NewMemorySessionStore()
	blacklist := NewMemoryTokenBlacklist()
	refreshStore := NewMemoryRefreshStore()

	svc := NewAuthServiceFromConfig(AuthServiceConfig{
		UserStore:    userStore,
		SessionStore: sessionStore,
		JWTSecret:    "test-secret-key-for-unit-tests",
		Blacklist:    blacklist,
		RefreshStore: refreshStore,
		AccessTTL:    2 * time.Second,
		RefreshTTL:   time.Hour,
	})

	user, pair, err := svc.Register("testuser", "test@example.com", "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if pair.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}

	ctx := context.Background()
	_, _, err = svc.ValidateToken(ctx, pair.AccessToken)
	if err != nil {
		t.Fatalf("validate access: %v", err)
	}

	newPair, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newPair.AccessToken == "" {
		t.Fatal("expected new access token")
	}

	if err := svc.Logout(ctx, pair.AccessToken, ""); err != nil {
		t.Fatalf("logout: %v", err)
	}
	_, _, err = svc.ValidateToken(ctx, pair.AccessToken)
	if err == nil {
		t.Fatal("expected revoked/expired token to fail")
	}

	_ = user
}

func TestAuth_ExpiredTokenRejected(t *testing.T) {
	svc := NewAuthServiceFromConfig(AuthServiceConfig{
		UserStore:    NewMemoryUserStore(),
		SessionStore: NewMemorySessionStore(),
		JWTSecret:    "test-secret",
		AccessTTL:    time.Millisecond,
		RefreshTTL:   time.Hour,
	})

	_, pair, err := svc.Register("expuser", "exp@example.com", "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	_, _, err = svc.ValidateToken(context.Background(), pair.AccessToken)
	if err == nil {
		t.Fatal("expected expired token error")
	}
}

func TestRBAC_HasRole(t *testing.T) {
	if !HasRole(RoleAdmin, RoleAdmin) {
		t.Fatal("admin should match admin")
	}
	if HasRole(RoleUser, RoleAdmin) {
		t.Fatal("user should not match admin")
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	// 仅验证角色上下文逻辑
	ctx := ContextWithRole(context.Background(), RoleUser)
	role, ok := RoleFromContext(ctx)
	if !ok || role != RoleUser {
		t.Fatalf("role=%v ok=%v", role, ok)
	}
}

func TestExtractBearerToken(t *testing.T) {
	token, ok := ExtractBearerToken("Bearer abc.def.ghi")
	if !ok || token != "abc.def.ghi" {
		t.Fatalf("token=%q ok=%v", token, ok)
	}
}

func TestParseAccessToken_WithJTI(t *testing.T) {
	svc := NewAuthServiceFromConfig(AuthServiceConfig{
		UserStore:    NewMemoryUserStore(),
		SessionStore: NewMemorySessionStore(),
		JWTSecret:    "secret",
		AccessTTL:    time.Minute,
	})
	user := &User{ID: "u1", Username: "u", Role: string(RoleUser)}
	pair, err := svc.issueTokenPair(context.Background(), user, "s1")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.parseAccessToken(pair.AccessToken)
	if err != nil || claims.JTI == "" {
		t.Fatalf("claims jti empty: %v", err)
	}
}

func TestJWTExpiryError(t *testing.T) {
	claims := jwt.MapClaims{
		"user_id": "u1",
		"exp":     time.Now().Add(-time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte("secret"))
	_, err := jwt.Parse(signed, func(t *jwt.Token) (interface{}, error) {
		return []byte("secret"), nil
	})
	if err == nil {
		t.Fatal("expected expired")
	}
	_ = httptest.NewRecorder()
}
