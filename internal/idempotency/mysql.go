package idempotency

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

type Store interface {
	TryAcquire(ctx context.Context, taskID string) (bool, error)
	MarkCompleted(ctx context.Context, taskID string) error
	Release(ctx context.Context, taskID string) error
}

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS task_execution (
			task_id VARCHAR(64) NOT NULL PRIMARY KEY,
			status VARCHAR(20) NOT NULL,
			created_at DATETIME NOT NULL,
			completed_at DATETIME NULL,
			last_error TEXT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create task_execution table: %w", err)
	}

	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) TryAcquire(ctx context.Context, taskID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO task_execution (task_id, status, created_at)
		VALUES (?, 'processing', NOW())
	`, taskID)
	if err != nil {
		return false, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return rows == 1, nil
}

func (s *MySQLStore) MarkCompleted(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE task_execution
		SET status = 'completed', completed_at = NOW(), last_error = NULL
		WHERE task_id = ?
	`, taskID)
	return err
}

func (s *MySQLStore) Release(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM task_execution WHERE task_id = ?`, taskID)
	return err
}
