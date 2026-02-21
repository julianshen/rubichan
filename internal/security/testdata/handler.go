package testdata

import "database/sql"

// GetUser is a TEST FIXTURE handler with SQL injection vulnerability.
func GetUser(db *sql.DB, id string) (*sql.Row, error) {
	row := db.QueryRow("SELECT * FROM users WHERE id=" + id)
	return row, nil
}
