package auth

import "errors"

// Role 用户角色。
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

var ErrForbidden = errors.New("权限不足")

// NormalizeRole 规范化角色字符串，未知角色降级为 user。
func NormalizeRole(role string) Role {
	switch Role(role) {
	case RoleAdmin:
		return RoleAdmin
	default:
		return RoleUser
	}
}

// HasRole 判断角色是否在允许列表中。
func HasRole(role Role, allowed ...Role) bool {
	for _, r := range allowed {
		if role == r {
			return true
		}
	}
	return false
}
