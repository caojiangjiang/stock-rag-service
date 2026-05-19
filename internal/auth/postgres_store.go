package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

type PostgresUserStore struct {
	db *pgxpool.Pool
}

func NewPostgresUserStore(db *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{db: db}
}

func (s *PostgresUserStore) InitTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	`
	_, err := s.db.Exec(context.Background(), query)
	return err
}

func (s *PostgresUserStore) GetByID(userID string) (*User, bool) {
	var user User
	err := s.db.QueryRow(context.Background(), `
		SELECT id, username, email, password, created_at, updated_at
		FROM users WHERE id = $1`, userID).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, false
	}
	return &user, true
}

func (s *PostgresUserStore) GetByUsername(username string) (*User, bool) {
	var user User
	err := s.db.QueryRow(context.Background(), `
		SELECT id, username, email, password, created_at, updated_at
		FROM users WHERE username = $1`, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, false
	}
	return &user, true
}

func (s *PostgresUserStore) GetByEmail(email string) (*User, bool) {
	var user User
	err := s.db.QueryRow(context.Background(), `
		SELECT id, username, email, password, created_at, updated_at
		FROM users WHERE email = $1`, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, false
	}
	return &user, true
}

func (s *PostgresUserStore) Set(user *User) error {
	_, err := s.db.Exec(context.Background(), `
		INSERT INTO users (id, username, email, password, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			email = EXCLUDED.email,
			password = EXCLUDED.password,
			updated_at = EXCLUDED.updated_at`,
		user.ID,
		user.Username,
		user.Email,
		user.Password,
		user.CreatedAt,
		user.UpdatedAt,
	)
	return err
}

func (s *PostgresUserStore) Delete(userID string) error {
	_, err := s.db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	return err
}

type PostgresSessionStore struct {
	db *pgxpool.Pool
}

func NewPostgresSessionStore(db *pgxpool.Pool) *PostgresSessionStore {
	return &PostgresSessionStore{db: db}
}

func (s *PostgresSessionStore) InitTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		last_active_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);
	
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	`
	_, err := s.db.Exec(context.Background(), query)
	return err
}

func (s *PostgresSessionStore) Get(sessionID string) (*Session, bool) {
	var session Session
	err := s.db.QueryRow(context.Background(), `
		SELECT id, user_id, last_active_at, created_at
		FROM sessions WHERE id = $1`, sessionID).Scan(
		&session.ID,
		&session.UserID,
		&session.LastActiveAt,
		&session.CreatedAt,
	)
	if err != nil {
		return nil, false
	}
	return &session, true
}

func (s *PostgresSessionStore) Set(sessionID string, session *Session) {
	s.db.Exec(context.Background(), `
		INSERT INTO sessions (id, user_id, last_active_at, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			last_active_at = EXCLUDED.last_active_at,
			created_at = EXCLUDED.created_at`,
		sessionID,
		session.UserID,
		session.LastActiveAt,
		session.CreatedAt,
	)
}

func (s *PostgresSessionStore) Delete(sessionID string) {
	s.db.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, sessionID)
}

func (s *PostgresSessionStore) ListByUserID(userID string) []*Session {
	rows, _ := s.db.Query(context.Background(), `
		SELECT id, user_id, last_active_at, created_at
		FROM sessions WHERE user_id = $1`, userID)
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.LastActiveAt,
			&session.CreatedAt,
		); err == nil {
			sessions = append(sessions, &session)
		}
	}
	return sessions
}

func (s *PostgresSessionStore) List() []*Session {
	rows, _ := s.db.Query(context.Background(), `
		SELECT id, user_id, last_active_at, created_at FROM sessions`)
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.LastActiveAt,
			&session.CreatedAt,
		); err == nil {
			sessions = append(sessions, &session)
		}
	}
	return sessions
}

func (s *PostgresSessionStore) UpdateLastActive(sessionID string) error {
	_, err := s.db.Exec(context.Background(), `
		UPDATE sessions SET last_active_at = $1 WHERE id = $2`,
		time.Now(),
		sessionID,
	)
	return err
}
