package store

import (
	"database/sql"
	"sync"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

type Store struct {
	db        *sql.DB
	taskLocks [taskLockShards]sync.Mutex
}

// Open creates or opens the SQLite database at dbPath, configures it for
// optimal single-file usage, and runs schema migrations.
//
// Use ":memory:" for testing.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if err = db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err = s.Migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for use by other packages (e.g. Registry).
func (s *Store) DB() *sql.DB {
	return s.db
}
