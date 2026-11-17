package router

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func tablesOf(t *testing.T, pool *pgxpool.Pool) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		out[n] = true
	}
	return out
}

// TestDatabaseSeparation: each database holds only its own tables (a dump or
// leaked DSN of one exposes nothing of the others), and integrity across
// databases is enforced by the application, failing closed.
func TestDatabaseSeparation(t *testing.T) {
	e := setup(t)
	owns := map[string]struct {
		pool        *pgxpool.Pool
		has, hasNot []string
	}{
		"core":     {e.pool, []string{"client", "bond", "member", "audit_outbox"}, []string{"app_user", "role", "action", "auth_refresh_token", "activity_log", "file_object", "document"}},
		"security": {e.secPool, []string{"app_user", "role", "action", "role_action", "auth_refresh_token", "audit_outbox"}, []string{"client", "activity_log", "file_object"}},
		"log":      {e.logPool, []string{"activity_log"}, []string{"app_user", "client", "file_object", "audit_outbox"}},
		"document": {e.docPool, []string{"file_object", "document", "document_authorization_table", "audit_outbox"}, []string{"app_user", "client", "activity_log"}},
	}
	for name, o := range owns {
		tables := tablesOf(t, o.pool)
		for _, tb := range o.has {
			if !tables[tb] {
				t.Errorf("%s must contain %s", name, tb)
			}
		}
		for _, tb := range o.hasNot {
			if tables[tb] {
				t.Errorf("%s must not contain %s", name, tb)
			}
		}
	}

	admin, _ := e.login("admin", adminPassword)
	client := e.create(admin, "/clients", map[string]any{"client_name": "Client A", "client_code": "CA"})["uuid"].(string)
	user := e.userWith(admin, "CLIENT-A", "client.a", "Client-A-Pass-1", client, "client:read")
	clientPath := "/clients/" + client
	e.expect(e.do("DELETE", "/api/v1"+clientPath, admin, nil, "If-Match", e.etag(admin, clientPath)), 409, "delete client used by a user")

	// A user whose client disappeared (no FK across databases) is refused,
	// never treated as "no tenant filter".
	if _, err := e.pool.Exec(context.Background(), "UPDATE client SET deleted_at = now() WHERE uuid = $1", client); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("GET", "/api/v1/clients", user, nil), 401, "user of a deleted client")

	// Audit events of each database reach pfm_log through the outbox.
	e.ship()
	var sources int
	_ = e.logPool.QueryRow(context.Background(), "SELECT count(DISTINCT source) FROM activity_log").Scan(&sources)
	if sources != 2 { // security (login, users, roles) and core (client)
		t.Fatalf("expected events from 2 source databases, got %d", sources)
	}
}
