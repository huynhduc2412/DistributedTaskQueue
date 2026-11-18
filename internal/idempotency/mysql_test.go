package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newMockStore(t *testing.T) (*MySQLStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create SQL mock: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return &MySQLStore{db: db}, mock
}

func TestTryAcquire(t *testing.T) {
	tests := []struct {
		name    string
		result  sql.Result
		err     error
		want    bool
		wantErr bool
	}{
		{name: "new task is acquired", result: sqlmock.NewResult(0, 1), want: true},
		{name: "existing task is not acquired", result: sqlmock.NewResult(0, 0), want: false},
		{name: "database error", err: errors.New("database unavailable"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := newMockStore(t)
			expectation := mock.ExpectExec("INSERT IGNORE INTO task_execution").WithArgs("job-123")
			if tt.err != nil {
				expectation.WillReturnError(tt.err)
			} else {
				expectation.WillReturnResult(tt.result)
			}

			got, err := store.TryAcquire(context.Background(), "job-123")
			if (err != nil) != tt.wantErr {
				t.Fatalf("TryAcquire() error = %v, want error=%t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("TryAcquire() = %t, want %t", got, tt.want)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkCompletedAndRelease(t *testing.T) {
	store, mock := newMockStore(t)
	mock.ExpectExec("UPDATE task_execution").WithArgs("job-123").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM task_execution").WithArgs("job-123").WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.MarkCompleted(context.Background(), "job-123"); err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}
	if err := store.Release(context.Background(), "job-123"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
