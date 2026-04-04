package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const DefaultSessionID = "default"

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrMissingDatabaseURL = errors.New("DATABASE_URL not set")
)

type Session struct {
	ID            string
	CookiesJSON   string
	CookiesHeader string
	HeadersJSON   string
	UserAgent     string
	DataJSON      string
	SourceURL     string
	Bookmark      string
	UpdatedAt     time.Time
	ExpiresAt     sql.NullTime
}

type SessionStore interface {
	GetSession(ctx context.Context, id string) (*Session, error)
	UpsertSession(ctx context.Context, session *Session) error
	UpdateBookmark(ctx context.Context, id string, bookmark string) error
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStoreFromEnv() (SessionStore, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, ErrMissingDatabaseURL
	}
	return NewPostgresStore(databaseURL)
}

func NewPostgresStore(databaseURL string) (SessionStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	store := &PostgresStore{db: db}
	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (p *PostgresStore) GetSession(ctx context.Context, id string) (*Session, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, cookies_json, cookies_header, headers_json, user_agent, data_json, source_url, bookmark, updated_at, expires_at
		FROM sessions
		WHERE id = $1
	`, id)

	session := &Session{}
	if err := row.Scan(
		&session.ID,
		&session.CookiesJSON,
		&session.CookiesHeader,
		&session.HeadersJSON,
		&session.UserAgent,
		&session.DataJSON,
		&session.SourceURL,
		&session.Bookmark,
		&session.UpdatedAt,
		&session.ExpiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return session, nil
}

func (p *PostgresStore) UpsertSession(ctx context.Context, session *Session) error {
	if session == nil {
		return errors.New("session is nil")
	}
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO sessions (id, cookies_json, cookies_header, headers_json, user_agent, data_json, source_url, bookmark, updated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			cookies_json = EXCLUDED.cookies_json,
			cookies_header = EXCLUDED.cookies_header,
			headers_json = EXCLUDED.headers_json,
			user_agent = EXCLUDED.user_agent,
			data_json = EXCLUDED.data_json,
			source_url = EXCLUDED.source_url,
			bookmark = EXCLUDED.bookmark,
			updated_at = EXCLUDED.updated_at,
			expires_at = EXCLUDED.expires_at
	`, session.ID, session.CookiesJSON, session.CookiesHeader, session.HeadersJSON, session.UserAgent, session.DataJSON, session.SourceURL, session.Bookmark, session.UpdatedAt, session.ExpiresAt)
	return err
}

func (p *PostgresStore) UpdateBookmark(ctx context.Context, id string, bookmark string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE sessions
		SET bookmark = $2, updated_at = $3
		WHERE id = $1
	`, id, bookmark, time.Now().UTC())
	return err
}

func (p *PostgresStore) ensureSchema() error {
	_, err := p.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			cookies_json TEXT NOT NULL,
			cookies_header TEXT NOT NULL,
			headers_json TEXT,
			user_agent TEXT,
			data_json TEXT,
			source_url TEXT,
			bookmark TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ
		)
	`)
	if err != nil {
		return err
	}
	if _, err := p.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS headers_json TEXT`); err != nil {
		return err
	}
	if _, err := p.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_agent TEXT`); err != nil {
		return err
	}
	if _, err := p.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS data_json TEXT`); err != nil {
		return err
	}
	if _, err := p.db.Exec(`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_url TEXT`); err != nil {
		return err
	}
	return nil
}
