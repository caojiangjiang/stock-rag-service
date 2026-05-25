package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"stock_rag/internal/auth"
)

type AuthHandler struct {
	authService auth.AuthService
	jwtSecret   string
}

func NewAuthHandler(authService auth.AuthService, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		jwtSecret:   jwtSecret,
	}
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || len(req.Username) < 3 || len(req.Username) > 20 {
		http.Error(w, "Username must be between 3 and 20 characters", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}
	if req.Password == "" || len(req.Password) < 6 {
		http.Error(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	user, pair, err := h.authService.Register(req.Username, req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeTokenResponse(w, user, pair)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	user, pair, err := h.authService.Login(req.Username, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	writeTokenResponse(w, user, pair)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RefreshToken == "" {
		http.Error(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	pair, err := h.authService.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrRefreshInvalid) {
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pair)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accessToken, _ := auth.ExtractBearerToken(r.Header.Get("Authorization"))
	var body LogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := h.authService.Logout(r.Context(), accessToken, body.RefreshToken); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "logout success"})
}

func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, ok := auth.ExtractBearerToken(r.Header.Get("Authorization"))
	if !ok || token == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	user, session, err := h.authService.ValidateToken(r.Context(), token)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrTokenRevoked) {
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user":    user,
		"session": session,
	})
}

func writeTokenResponse(w http.ResponseWriter, user *auth.User, pair *auth.TokenPair) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"user": user,
	}
	if pair != nil {
		payload["access_token"] = pair.AccessToken
		payload["refresh_token"] = pair.RefreshToken
		payload["token_type"] = pair.TokenType
		payload["expires_in"] = pair.ExpiresIn
		// 兼容旧客户端
		payload["token"] = pair.AccessToken
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func RegisterAuthRoutes(mux *http.ServeMux, authService auth.AuthService, jwtSecret string) {
	authHandler := NewAuthHandler(authService, jwtSecret)

	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)
	mux.HandleFunc("/api/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/auth/me", authHandler.GetCurrentUser)
}
