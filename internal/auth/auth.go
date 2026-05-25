package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidToken   = errors.New("invalid or expired token")
	ErrTokenRevoked   = errors.New("token has been revoked")
	ErrRefreshInvalid = errors.New("invalid refresh token")
)

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Password  string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserStore interface {
	GetByID(userID string) (*User, bool)
	GetByUsername(username string) (*User, bool)
	GetByEmail(email string) (*User, bool)
	Set(user *User) error
	Delete(userID string) error
}

type MemoryUserStore struct {
	users map[string]*User
	mutex sync.RWMutex
}

func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users: make(map[string]*User),
		mutex: sync.RWMutex{},
	}
}

func (s *MemoryUserStore) GetByID(userID string) (*User, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	user, exists := s.users[userID]
	return user, exists
}

func (s *MemoryUserStore) GetByUsername(username string) (*User, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, user := range s.users {
		if user.Username == username {
			return user, true
		}
	}
	return nil, false
}

func (s *MemoryUserStore) GetByEmail(email string) (*User, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for _, user := range s.users {
		if user.Email == email {
			return user, true
		}
	}
	return nil, false
}

func (s *MemoryUserStore) Set(user *User) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.users[user.ID] = user
	return nil
}

func (s *MemoryUserStore) Delete(userID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.users, userID)
	return nil
}

func HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

func VerifyPassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

type Session struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	LastActiveAt time.Time `json:"last_active_at"`
	CreatedAt    time.Time `json:"created_at"`
	mutex        sync.RWMutex
}

type SessionStore interface {
	Get(sessionID string) (*Session, bool)
	Set(sessionID string, session *Session)
	Delete(sessionID string)
	ListByUserID(userID string) []*Session
	List() []*Session
}

type MemorySessionStore struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]*Session),
		mutex:    sync.RWMutex{},
	}
}

func (s *MemorySessionStore) Get(sessionID string) (*Session, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	session, exists := s.sessions[sessionID]
	return session, exists
}

func (s *MemorySessionStore) Set(sessionID string, session *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.sessions[sessionID] = session
}

func (s *MemorySessionStore) Delete(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, sessionID)
}

func (s *MemorySessionStore) ListByUserID(userID string) []*Session {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var sessions []*Session
	for _, session := range s.sessions {
		if session.UserID == userID {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

func (s *MemorySessionStore) List() []*Session {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var sessions []*Session
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// AuthService 认证服务接口。
type AuthService interface {
	Register(username, email, password string) (*User, *TokenPair, error)
	Login(username, password string) (*User, *TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	Logout(ctx context.Context, accessToken, refreshToken string) error
	ValidateToken(ctx context.Context, token string) (*User, *Session, error)
}

// AuthServiceConfig 认证服务配置。
type AuthServiceConfig struct {
	UserStore     UserStore
	SessionStore  SessionStore
	JWTSecret     string
	Blacklist     TokenBlacklist
	RefreshStore  RefreshStore
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	AdminUsername string
}

// AuthServiceImpl 认证服务实现。
type AuthServiceImpl struct {
	userStore     UserStore
	sessionStore  SessionStore
	jwtSecret     string
	blacklist     TokenBlacklist
	refreshStore  RefreshStore
	accessTTL     time.Duration
	refreshTTL    time.Duration
	adminUsername string
}

func NewAuthServiceImpl(userStore UserStore, sessionStore SessionStore, jwtSecret string) *AuthServiceImpl {
	return NewAuthServiceFromConfig(AuthServiceConfig{
		UserStore:    userStore,
		SessionStore: sessionStore,
		JWTSecret:    jwtSecret,
	})
}

func NewAuthServiceFromConfig(cfg AuthServiceConfig) *AuthServiceImpl {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = loadAccessTTL()
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = loadRefreshTTL()
	}
	if cfg.AdminUsername == "" {
		cfg.AdminUsername = strings.TrimSpace(os.Getenv("ADMIN_USERNAME"))
	}
	return &AuthServiceImpl{
		userStore:     cfg.UserStore,
		sessionStore:  cfg.SessionStore,
		jwtSecret:     cfg.JWTSecret,
		blacklist:     cfg.Blacklist,
		refreshStore:  cfg.RefreshStore,
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
		adminUsername: cfg.AdminUsername,
	}
}

func loadAccessTTL() time.Duration {
	if v := strings.TrimSpace(os.Getenv("JWT_ACCESS_TTL_MINUTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 15 * time.Minute
}

func loadRefreshTTL() time.Duration {
	if v := strings.TrimSpace(os.Getenv("JWT_REFRESH_TTL_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	return 7 * 24 * time.Hour
}

func (a *AuthServiceImpl) resolveRole(username string) string {
	if a.adminUsername != "" && strings.EqualFold(username, a.adminUsername) {
		return string(RoleAdmin)
	}
	return string(RoleUser)
}

func (a *AuthServiceImpl) Register(username, email, password string) (*User, *TokenPair, error) {
	if _, exists := a.userStore.GetByUsername(username); exists {
		return nil, nil, errors.New("用户名已存在")
	}
	if _, exists := a.userStore.GetByEmail(email); exists {
		return nil, nil, errors.New("邮箱已存在")
	}

	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, nil, err
	}

	user := &User{
		ID:        uuid.New().String(),
		Username:  username,
		Email:     email,
		Role:      a.resolveRole(username),
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := a.userStore.Set(user); err != nil {
		return nil, nil, err
	}

	sessionID := uuid.New().String()
	a.sessionStore.Set(sessionID, &Session{
		ID: sessionID, UserID: user.ID,
		LastActiveAt: time.Now(), CreatedAt: time.Now(),
	})

	pair, err := a.issueTokenPair(context.Background(), user, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

func (a *AuthServiceImpl) Login(username, password string) (*User, *TokenPair, error) {
	user, exists := a.userStore.GetByUsername(username)
	if !exists {
		return nil, nil, errors.New("用户不存在")
	}
	if err := VerifyPassword(user.Password, password); err != nil {
		return nil, nil, errors.New("密码错误")
	}

	sessionID := uuid.New().String()
	a.sessionStore.Set(sessionID, &Session{
		ID: sessionID, UserID: user.ID,
		LastActiveAt: time.Now(), CreatedAt: time.Now(),
	})

	pair, err := a.issueTokenPair(context.Background(), user, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

func (a *AuthServiceImpl) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if a.refreshStore == nil || refreshToken == "" {
		return nil, ErrRefreshInvalid
	}
	record, ok := a.refreshStore.Get(ctx, refreshToken)
	if !ok {
		return nil, ErrRefreshInvalid
	}
	user, exists := a.userStore.GetByID(record.UserID)
	if !exists {
		return nil, ErrRefreshInvalid
	}
	session, exists := a.sessionStore.Get(record.SessionID)
	if !exists {
		return nil, ErrRefreshInvalid
	}

	_ = a.refreshStore.Delete(ctx, refreshToken)

	session.mutex.Lock()
	session.LastActiveAt = time.Now()
	session.mutex.Unlock()

	return a.issueTokenPair(ctx, user, session.ID)
}

func (a *AuthServiceImpl) Logout(ctx context.Context, accessToken, refreshToken string) error {
	if accessToken != "" {
		claims, err := a.parseAccessToken(accessToken)
		if err == nil {
			if claims.JTI != "" && a.blacklist != nil && claims.ExpiresAt != nil {
				ttl := time.Until(claims.ExpiresAt.Time)
				if ttl > 0 {
					_ = a.blacklist.Revoke(ctx, claims.JTI, ttl)
				}
			}
			a.sessionStore.Delete(claims.SessionID)
			if a.refreshStore != nil {
				_ = a.refreshStore.DeleteBySession(ctx, claims.SessionID)
			}
		}
	}
	if refreshToken != "" && a.refreshStore != nil {
		_ = a.refreshStore.Delete(ctx, refreshToken)
	}
	return nil
}

func (a *AuthServiceImpl) ValidateToken(ctx context.Context, token string) (*User, *Session, error) {
	claims, err := a.parseAccessToken(token)
	if err != nil {
		legacy, mapClaims, mapErr := parseLegacyMapClaims(token, a.jwtSecret)
		if mapErr != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				return nil, nil, ErrInvalidToken
			}
			return nil, nil, ErrInvalidToken
		}
		claims = mapClaims
		if claims.JTI != "" && a.blacklist != nil {
			revoked, _ := a.blacklist.IsRevoked(ctx, claims.JTI)
			if revoked {
				return nil, nil, ErrTokenRevoked
			}
		}
		user, exists := a.userStore.GetByID(legacy.UserID)
		if !exists {
			return nil, nil, errors.New("user not found")
		}
		session, exists := a.sessionStore.Get(legacy.SessionID)
		if !exists {
			return nil, nil, errors.New("session not found")
		}
		return user, session, nil
	}

	if claims.JTI != "" && a.blacklist != nil {
		revoked, err := a.blacklist.IsRevoked(ctx, claims.JTI)
		if err != nil {
			return nil, nil, err
		}
		if revoked {
			return nil, nil, ErrTokenRevoked
		}
	}

	user, exists := a.userStore.GetByID(claims.UserID)
	if !exists {
		return nil, nil, errors.New("user not found")
	}

	session, exists := a.sessionStore.Get(claims.SessionID)
	if !exists {
		return nil, nil, errors.New("session not found")
	}

	session.mutex.Lock()
	session.LastActiveAt = time.Now()
	session.mutex.Unlock()

	return user, session, nil
}

type UserClaims struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
	Role      Role   `json:"role"`
}

type contextKey string

const (
	UserIDKey contextKey = "user_id"
	RoleKey   contextKey = "role"
)

func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok
}

func ContextWithRole(ctx context.Context, role Role) context.Context {
	return context.WithValue(ctx, RoleKey, role)
}

func RoleFromContext(ctx context.Context) (Role, bool) {
	role, ok := ctx.Value(RoleKey).(Role)
	return role, ok
}

func ContextWithPrincipal(ctx context.Context, user *User, session *Session) context.Context {
	ctx = ContextWithUserID(ctx, user.ID)
	ctx = ContextWithRole(ctx, NormalizeRole(user.Role))
	return context.WithValue(ctx, contextKey("session_id"), session.ID)
}

func ParseUserFromToken(tokenString string, jwtSecret string) (*UserClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims, ok := token.Claims.(*AccessClaims); ok {
		return &UserClaims{
			UserID: claims.UserID, Username: claims.Username,
			SessionID: claims.SessionID, Role: claims.Role,
		}, nil
	}

	legacy, ac, err := parseLegacyMapClaims(tokenString, jwtSecret)
	if err != nil {
		return nil, err
	}
	if ac.Role != "" {
		legacy.Role = ac.Role
	}
	return legacy, nil
}

// ExtractBearerToken 从 Authorization 头解析 Bearer token。
func ExtractBearerToken(authorization string) (string, bool) {
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(authorization) > len(prefix) && strings.EqualFold(authorization[:len(prefix)], prefix) {
		return strings.TrimSpace(authorization[len(prefix):]), true
	}
	return authorization, true
}
