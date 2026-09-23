package store

import (
	"context"
	_ "embed"
	"encoding/json"
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
	cardsJSON, err := json.Marshal(s.Cards)
	if err != nil {
		return Submission{}, fmt.Errorf("store: submission cards の直列化に失敗しました: %w", err)
	}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO submissions (discord_id, display_name, branch, pr_number, pr_url, status, cards)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		RETURNING id, created_at, updated_at`,
		s.DiscordID, s.DisplayName, s.Branch, s.PRNumber, s.PRURL, s.Status, cardsJSON).
		Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (p *Postgres) ListSubmissions(ctx context.Context, limit int) ([]Submission, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, discord_id, display_name, branch, pr_number, pr_url, status, cards, created_at, updated_at
		FROM submissions ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Submission
	for rows.Next() {
		var s Submission
		var cardsJSON []byte
		if err := rows.Scan(&s.ID, &s.DiscordID, &s.DisplayName,
			&s.Branch, &s.PRNumber, &s.PRURL, &s.Status, &cardsJSON, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(cardsJSON, &s.Cards); err != nil {
			return nil, fmt.Errorf("store: submission cards の復元に失敗しました: %w", err)
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

func (p *Postgres) CreateDraft(ctx context.Context, d Draft) (Draft, error) {
	taxonomy, err := json.Marshal(d.Taxonomy)
	if err != nil {
		return Draft{}, err
	}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO drafts (discord_id, display_name, kind, card_id, name, fruit, description,
			cost, hp, attack, effect, role, sweet_type, version, taxonomy, image_slug,
			webp, ogp_png, source_bytes, source_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING id, created_at`,
		d.DiscordID, d.DisplayName, d.Kind, d.CardID, d.Name, d.Fruit, d.Description,
		d.Cost, d.HP, d.Attack, d.Effect, d.Role, d.SweetType, d.Version, taxonomy, d.ImageSlug,
		d.WebP, d.OGPPNG, d.SourceBytes, d.SourceType).
		Scan(&d.ID, &d.CreatedAt)
	return d, err
}

const draftColumns = `id, discord_id, display_name, kind, card_id, name, fruit, description,
	cost, hp, attack, effect, role, sweet_type, version, taxonomy, image_slug,
	webp, ogp_png, source_bytes, source_type, created_at`

func scanDraft(row pgx.Row) (Draft, error) {
	var d Draft
	var taxonomy []byte
	err := row.Scan(&d.ID, &d.DiscordID, &d.DisplayName, &d.Kind, &d.CardID, &d.Name, &d.Fruit, &d.Description,
		&d.Cost, &d.HP, &d.Attack, &d.Effect, &d.Role, &d.SweetType, &d.Version, &taxonomy, &d.ImageSlug,
		&d.WebP, &d.OGPPNG, &d.SourceBytes, &d.SourceType, &d.CreatedAt)
	if err == nil && len(taxonomy) > 0 {
		err = json.Unmarshal(taxonomy, &d.Taxonomy)
	}
	return d, err
}

func (p *Postgres) ListDraftsByUser(ctx context.Context, discordID string) ([]Draft, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+draftColumns+`
		FROM drafts WHERE discord_id = $1 ORDER BY id`, discordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Draft
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *Postgres) GetDraft(ctx context.Context, id int64) (Draft, error) {
	d, err := scanDraft(p.pool.QueryRow(ctx, `SELECT `+draftColumns+` FROM drafts WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	return d, err
}

func (p *Postgres) DeleteDrafts(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM drafts WHERE id = ANY($1)`, ids)
	return err
}
