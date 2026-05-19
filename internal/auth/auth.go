package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
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

type AuthService interface {
	Register(username, email, password string) (*User, string, error)
	Login(username, password string) (*User, string, error)
	Logout(sessionID string) error
	ValidateToken(token string) (*User, *Session, error)
}

type AuthServiceImpl struct {
	userStore    UserStore
	sessionStore SessionStore
	jwtSecret    string
}

func NewAuthServiceImpl(userStore UserStore, sessionStore SessionStore, jwtSecret string) *AuthServiceImpl {
	return &AuthServiceImpl{
		userStore:    userStore,
		sessionStore: sessionStore,
		jwtSecret:    jwtSecret,
	}
}

func (a *AuthServiceImpl) Register(username, email, password string) (*User, string, error) {
	if _, exists := a.userStore.GetByUsername(username); exists {
		return nil, "", errors.New("用户名已存在")
	}

	if _, exists := a.userStore.GetByEmail(email); exists {
		return nil, "", errors.New("邮箱已存在")
	}

	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, "", err
	}

	user := &User{
		ID:        uuid.New().String(),
		Username:  username,
		Email:     email,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := a.userStore.Set(user); err != nil {
		return nil, "", err
	}

	sessionID := uuid.New().String()
	session := &Session{
		ID:           sessionID,
		UserID:       user.ID,
		LastActiveAt: time.Now(),
		CreatedAt:    time.Now(),
	}
	a.sessionStore.Set(sessionID, session)

	token, err := a.generateJWT(user, sessionID)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

func (a *AuthServiceImpl) Login(username, password string) (*User, string, error) {
	user, exists := a.userStore.GetByUsername(username)
	if !exists {
		return nil, "", errors.New("用户不存在")
	}

	if err := VerifyPassword(user.Password, password); err != nil {
		return nil, "", errors.New("密码错误")
	}

	sessionID := uuid.New().String()
	session := &Session{
		ID:           sessionID,
		UserID:       user.ID,
		LastActiveAt: time.Now(),
		CreatedAt:    time.Now(),
	}
	a.sessionStore.Set(sessionID, session)

	token, err := a.generateJWT(user, sessionID)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

func (a *AuthServiceImpl) generateJWT(user *User, sessionID string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":    user.ID,
		"username":   user.Username,
		"session_id": sessionID,
		"exp":        time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.jwtSecret))
}

func (a *AuthServiceImpl) Logout(sessionID string) error {
	a.sessionStore.Delete(sessionID)
	return nil
}

func (a *AuthServiceImpl) ValidateToken(token string) (*User, *Session, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(a.jwtSecret), nil
	})

	if err != nil {
		return nil, nil, err
	}

	if !parsedToken.Valid {
		return nil, nil, errors.New("invalid token")
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return nil, nil, errors.New("invalid claims")
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, nil, errors.New("invalid user_id")
	}

	sessionID, ok := claims["session_id"].(string)
	if !ok {
		return nil, nil, errors.New("invalid session_id")
	}

	user, exists := a.userStore.GetByID(userID)
	if !exists {
		return nil, nil, errors.New("user not found")
	}

	session, exists := a.sessionStore.Get(sessionID)
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
}

type contextKey string

const UserIDKey contextKey = "user_id"

func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok
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
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	return &UserClaims{
		UserID:    claims["user_id"].(string),
		Username:  claims["username"].(string),
		SessionID: claims["session_id"].(string),
	}, nil
}
