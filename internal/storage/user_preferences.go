package storage

import (
 "context"
 "database/sql"
 "errors"
)

// Preferences are scoped to the authenticated username and namespace.
// The username also works with legacy single-account deployments.
func (d *DB) InitUserPreferences(ctx context.Context) error {
 _, err := d.execContext(ctx, `CREATE TABLE IF NOT EXISTS user_preferences (
 username VARCHAR(255) NOT NULL,
 namespace VARCHAR(64) NOT NULL,
 config_json TEXT NOT NULL,
 PRIMARY KEY (username, namespace)
 )`)
 return err
}

func (d *DB) GetUserPreference(ctx context.Context, username, namespace string) (string, bool, error) {
 var value string
 err := d.queryRowContext(ctx, "SELECT config_json FROM user_preferences WHERE username=? AND namespace=?", username, namespace).Scan(&value)
 if errors.Is(err, sql.ErrNoRows) { return "", false, nil }
 return value, err == nil, err
}

func (d *DB) SetUserPreference(ctx context.Context, username, namespace, value string) error {
 // UPDATE first avoids replacing an existing row on MySQL, SQLite and PostgreSQL.
 res, err := d.execContext(ctx, "UPDATE user_preferences SET config_json=? WHERE username=? AND namespace=?", value, username, namespace)
 if err != nil { return err }
 n, err := res.RowsAffected()
 if err != nil { return err }
 if n > 0 { return nil }
 _, err = d.execContext(ctx, "INSERT INTO user_preferences (username, namespace, config_json) VALUES (?, ?, ?)", username, namespace, value)
 return err
}
