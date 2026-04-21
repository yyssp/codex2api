package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSQLiteInitializesFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	if got := db.Driver(); got != "sqlite" {
		t.Fatalf("Driver() = %q, want %q", got, "sqlite")
	}
}

func TestSQLiteUsageLogsHasAPIKeyColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	columns, err := db.sqliteTableColumns(context.Background(), "usage_logs")
	if err != nil {
		t.Fatalf("sqliteTableColumns 返回错误: %v", err)
	}

	for _, name := range []string{"api_key_id", "api_key_name", "api_key_masked"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("usage_logs 缺少列 %q", name)
		}
	}
}

func TestUsageLogsFilterByAPIKeyID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	targetAPIKeyID := int64(7)

	logs := []*UsageLogInput{
		{
			AccountID:    1,
			Endpoint:     "/v1/chat/completions",
			Model:        "gpt-5.4",
			StatusCode:   200,
			DurationMs:   120,
			APIKeyID:     targetAPIKeyID,
			APIKeyName:   "Team A",
			APIKeyMasked: "sk-a****...****1111",
		},
		{
			AccountID:    1,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.4",
			StatusCode:   200,
			DurationMs:   220,
			APIKeyID:     targetAPIKeyID,
			APIKeyName:   "Team A",
			APIKeyMasked: "sk-a****...****1111",
		},
		{
			AccountID:    2,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.4-mini",
			StatusCode:   200,
			DurationMs:   320,
			APIKeyID:     8,
			APIKeyName:   "Team B",
			APIKeyMasked: "sk-b****...****2222",
		},
	}

	for _, usageLog := range logs {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	recentLogs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(recentLogs) != len(logs) {
		t.Fatalf("recentLogs 长度 = %d, want %d", len(recentLogs), len(logs))
	}

	foundSnapshot := false
	for _, usageLog := range recentLogs {
		if usageLog.APIKeyID == targetAPIKeyID {
			foundSnapshot = true
			if usageLog.APIKeyName != "Team A" {
				t.Fatalf("APIKeyName = %q, want %q", usageLog.APIKeyName, "Team A")
			}
			if usageLog.APIKeyMasked != "sk-a****...****1111" {
				t.Fatalf("APIKeyMasked = %q, want %q", usageLog.APIKeyMasked, "sk-a****...****1111")
			}
		}
	}
	if !foundSnapshot {
		t.Fatal("未找到带 API 密钥快照的最近日志")
	}

	page, err := db.ListUsageLogsByTimeRangePaged(ctx, UsageLogFilter{
		Start:    now.Add(-1 * time.Hour),
		End:      now.Add(1 * time.Hour),
		Page:     1,
		PageSize: 10,
		APIKeyID: &targetAPIKeyID,
	})
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged 返回错误: %v", err)
	}

	if page.Total != 2 {
		t.Fatalf("page.Total = %d, want %d", page.Total, 2)
	}
	if len(page.Logs) != 2 {
		t.Fatalf("len(page.Logs) = %d, want %d", len(page.Logs), 2)
	}
	for _, usageLog := range page.Logs {
		if usageLog.APIKeyID != targetAPIKeyID {
			t.Fatalf("APIKeyID = %d, want %d", usageLog.APIKeyID, targetAPIKeyID)
		}
		if usageLog.APIKeyName != "Team A" {
			t.Fatalf("APIKeyName = %q, want %q", usageLog.APIKeyName, "Team A")
		}
	}
}

func TestSQLiteCredentialUniqueIndexesPreventDuplicateActiveAccounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	rtID, err := db.InsertAccount(ctx, "rt-1", "refresh-token-1", "")
	if err != nil {
		t.Fatalf("InsertAccount 返回错误: %v", err)
	}
	if _, err := db.InsertAccount(ctx, "rt-dup", "refresh-token-1", ""); err == nil {
		t.Fatal("duplicate refresh token insert succeeded, want error")
	}

	atID, err := db.InsertATAccount(ctx, "at-1", "access-token-1", "")
	if err != nil {
		t.Fatalf("InsertATAccount 返回错误: %v", err)
	}
	if _, err := db.InsertATAccount(ctx, "at-dup", "access-token-1", ""); err == nil {
		t.Fatal("duplicate access token insert succeeded, want error")
	}

	if _, err := db.conn.ExecContext(ctx, `UPDATE accounts SET status = 'deleted' WHERE id IN ($1, $2)`, rtID, atID); err != nil {
		t.Fatalf("UPDATE deleted 返回错误: %v", err)
	}

	if _, err := db.InsertAccount(ctx, "rt-reuse", "refresh-token-1", ""); err != nil {
		t.Fatalf("InsertAccount after delete returned error: %v", err)
	}
	if _, err := db.InsertATAccount(ctx, "at-reuse", "access-token-1", ""); err != nil {
		t.Fatalf("InsertATAccount after delete returned error: %v", err)
	}
}

func TestSQLiteHistoricalDuplicateCredentialsDoNotBlockMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-duplicates.db")

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open 返回错误: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT DEFAULT '',
		credentials TEXT NOT NULL DEFAULT '{}',
		status TEXT DEFAULT 'active'
	);`); err != nil {
		t.Fatalf("CREATE TABLE 返回错误: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO accounts (name, credentials, status) VALUES
		('dup-1', '{"refresh_token":"same-rt"}', 'active'),
		('dup-2', '{"refresh_token":"same-rt"}', 'active'),
		('dup-3', '{"access_token":"same-at"}', 'active'),
		('dup-4', '{"access_token":"same-at"}', 'active')
	`); err != nil {
		t.Fatalf("INSERT duplicates 返回错误: %v", err)
	}

	db := &DB{conn: conn, driver: "sqlite"}
	if err := db.ensureSQLiteCredentialUniqueIndexes(ctx); err != nil {
		t.Fatalf("ensureSQLiteCredentialUniqueIndexes 返回错误: %v", err)
	}
}
