package main

import "testing"

func TestResolveJWTSecretAllowsDefaultOutsideProduction(t *testing.T) {
	secret, err := resolveJWTSecret("", "development")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret == "" {
		t.Fatal("expected fallback secret")
	}
}

func TestResolveJWTSecretRejectsMissingProductionSecret(t *testing.T) {
	if _, err := resolveJWTSecret("", "production"); err == nil {
		t.Fatal("expected production secret error")
	}
}

func TestResolveJWTSecretRejectsWeakProductionSecret(t *testing.T) {
	if _, err := resolveJWTSecret("short-secret", "prod"); err == nil {
		t.Fatal("expected weak production secret error")
	}
}

func TestResolveJWTSecretRejectsPlaceholderProductionSecret(t *testing.T) {
	if _, err := resolveJWTSecret("your_jwt_secret_key_here_make_it_long_and_random", "production"); err == nil {
		t.Fatal("expected placeholder production secret error")
	}
}

func TestResolveJWTSecretAcceptsStrongProductionSecret(t *testing.T) {
	const want = "0123456789abcdef0123456789abcdef"
	got, err := resolveJWTSecret(want, "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
