package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ieyoukan/pplale-cms/internal/auth"
)

//go:embed schema.sql
var schema string

// Postgres is the production Store.
type Postgres struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects, applies the schema and returns the store.
func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: データベースに接続できません: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: データベースに接続できません: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: スキーマの適用に失敗しました: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) SaveSession(ctx context.Context, s auth.Session) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, discord_id, display_name, csrf_token, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token_hash) DO UPDATE
		SET expires_at = EXCLUDED.expires_at, display_name = EXCLUDED.display_name`,
		s.TokenHash, s.DiscordID, s.DisplayName, s.CSRFToken, s.CreatedAt, s.ExpiresAt)
	return err
}

func (p *Postgres) FindSession(ctx context.Context, tokenHash string) (auth.Session, error) {
	var s auth.Session
	err := p.pool.QueryRow(ctx, `
		SELECT token_hash, discord_id, display_name, csrf_token, created_at, expires_at
		FROM sessions WHERE token_hash = $1`, tokenHash).
		Scan(&s.TokenHash, &s.DiscordID, &s.DisplayName, &s.CSRFToken, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, ErrNotFound
	}
	return s, err
}

func (p *Postgres) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (p *Postgres) DeleteSessionsByDiscordID(ctx context.Context, discordID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE discord_id = $1`, discordID)
	return err
}

// PurgeExpiredSessions removes sessions that are past their expiry.
func (p *Postgres) PurgeExpiredSessions(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, time.Now())
	return err
}

func (p *Postgres) GetUser(ctx context.Context, discordID string) (User, error) {
	var u User
	err := p.pool.QueryRow(ctx, `
		SELECT discord_id, display_name, role, added_by, created_at
		FROM users WHERE discord_id = $1`, discordID).
		Scan(&u.DiscordID, &u.DisplayName, &u.Role, &u.AddedBy, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (p *Postgres) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT discord_id, display_name, role, added_by, created_at
		FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.DiscordID, &u.DisplayName, &u.Role, &u.AddedBy, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) UpsertUser(ctx context.Context, u User) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO users (discord_id, display_name, role, added_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (discord_id) DO UPDATE
		SET display_name = EXCLUDED.display_name, role = EXCLUDED.role, added_by = EXCLUDED.added_by`,
		u.DiscordID, u.DisplayName, u.Role, u.AddedBy)
	return err
}

func (p *Postgres) DeleteUser(ctx context.Context, discordID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM users WHERE discord_id = $1`, discordID)
	return err
}

func (p *Postgres) CreateSubmission(ctx context.Context, s Submission) (Submission, error) {
	err := p.pool.QueryRow(ctx, `
		INSERT INTO submissions (discord_id, display_name, kind, card_id, card_name, branch, pr_number, pr_url, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		s.DiscordID, s.DisplayName, s.Kind, s.CardID, s.CardName, s.Branch, s.PRNumber, s.PRURL, s.Status).
		Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (p *Postgres) ListSubmissions(ctx context.Context, limit int) ([]Submission, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, discord_id, display_name, kind, card_id, card_name, branch, pr_number, pr_url, status, created_at, updated_at
		FROM submissions ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Submission
	for rows.Next() {
		var s Submission
		if err := rows.Scan(&s.ID, &s.DiscordID, &s.DisplayName, &s.Kind, &s.CardID, &s.CardName,
			&s.Branch, &s.PRNumber, &s.PRURL, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateSubmissionStatusByPR(ctx context.Context, prNumber int, status SubmissionStatus) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE submissions SET status = $1, updated_at = now() WHERE pr_number = $2`, status, prNumber)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
