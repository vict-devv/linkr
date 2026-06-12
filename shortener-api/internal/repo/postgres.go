package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresRepo(ctx context.Context, dsn string) (*PostgresRepo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresRepo{pool: pool}, nil
}

func (r *PostgresRepo) Save(ctx context.Context, longURL, code string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO urls (code, long_url) VALUES ($1, $2)`,
		code, longURL,
	)
	return err
}

func (r *PostgresRepo) Find(ctx context.Context, code string) (string, error) {
	var longURL string
	err := r.pool.QueryRow(ctx,
		`SELECT long_url FROM urls WHERE code = $1`,
		code,
	).Scan(&longURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return longURL, err
}

func (r *PostgresRepo) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint violation.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
