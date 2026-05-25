package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const tokenTypeAccess = "access"

// TokenPair 访问令牌与刷新令牌。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// AccessClaims 访问令牌声明。
type AccessClaims struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
	Role      Role   `json:"role"`
	JTI       string `json:"jti"`
	jwt.RegisteredClaims
}

func (a *AuthServiceImpl) issueTokenPair(ctx context.Context, user *User, sessionID string) (*TokenPair, error) {
	jti := uuid.New().String()
	accessTTL := a.accessTTL
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := a.refreshTTL
	if refreshTTL <= 0 {
		refreshTTL = 7 * 24 * time.Hour
	}

	now := time.Now()
	claims := AccessClaims{
		UserID:    user.ID,
		Username:  user.Username,
		SessionID: sessionID,
		Role:      NormalizeRole(user.Role),
		JTI:       jti,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(a.jwtSecret))
	if err != nil {
		return nil, err
	}

	refreshToken := uuid.New().String()
	if a.refreshStore != nil {
		record := RefreshRecord{
			UserID:    user.ID,
			SessionID: sessionID,
			ExpiresAt: now.Add(refreshTTL),
		}
		if err := a.refreshStore.Save(ctx, refreshToken, record, refreshTTL); err != nil {
			return nil, err
		}
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTTL.Seconds()),
	}, nil
}

func (a *AuthServiceImpl) parseAccessToken(tokenString string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(a.jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.JTI == "" {
		claims.JTI = claims.RegisteredClaims.ID
	}
	return claims, nil
}

func parseLegacyMapClaims(tokenString, jwtSecret string) (*UserClaims, *AccessClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !token.Valid {
		return nil, nil, errors.New("invalid token")
	}
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, nil, errors.New("invalid claims")
	}
	userID, _ := mapClaims["user_id"].(string)
	username, _ := mapClaims["username"].(string)
	sessionID, _ := mapClaims["session_id"].(string)
	roleStr, _ := mapClaims["role"].(string)
	jti, _ := mapClaims["jti"].(string)
	if jti == "" {
		jti, _ = mapClaims["id"].(string)
	}
	legacy := &UserClaims{
		UserID: userID, Username: username, SessionID: sessionID,
		Role: NormalizeRole(roleStr),
	}
	ac := &AccessClaims{
		UserID: userID, Username: username, SessionID: sessionID,
		Role: NormalizeRole(roleStr), JTI: jti,
	}
	return legacy, ac, nil
}
