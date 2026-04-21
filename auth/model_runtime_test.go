package auth

import (
	"testing"
	"time"
)

func TestNextForSessionForModelFallsBackWhenBoundAccountModelUnavailable(t *testing.T) {
	store := &Store{
		accounts: []*Account{
			{DBID: 1, AccessToken: "tok-1"},
			{DBID: 2, AccessToken: "tok-2"},
		},
		maxConcurrency:  1,
		sessionBindings: make(map[string]sessionAffinity),
	}
	store.bindSessionAffinity("session-1", store.accounts[1], "http://proxy-2")
	store.MarkModelUnavailable(store.accounts[1], "gpt-5.4", time.Hour, "model_not_found", 404)

	acc, proxyURL := store.NextForSessionForModel("session-1", nil, "gpt-5.4")
	if acc == nil {
		t.Fatal("expected fallback account")
	}
	if acc.DBID != 1 {
		t.Fatalf("account DBID = %d, want %d", acc.DBID, 1)
	}
	if proxyURL != "" {
		t.Fatalf("proxyURL = %q, want empty fallback proxy", proxyURL)
	}
	store.Release(acc)
}

func TestReportRequestSuccessForModelClearsModelState(t *testing.T) {
	store := &Store{maxConcurrency: 1}
	acc := &Account{DBID: 1, AccessToken: "tok-1"}

	store.MarkModelCooldown(acc, "gpt-5.4", 30*time.Minute, "rate_limited", 429)
	if acc.IsModelAvailable("gpt-5.4") {
		t.Fatal("model should be unavailable before success")
	}

	store.ReportRequestSuccessForModel(acc, "gpt-5.4", time.Second)

	if !acc.IsModelAvailable("gpt-5.4") {
		t.Fatal("model state should be cleared after success")
	}
}

func TestAcquireProxyFallsBackWhenPreferredEnhancedProxyUnavailable(t *testing.T) {
	store := &Store{
		proxyPoolEnabled: true,
		proxyPool:        []string{"http://proxy1:8080", "http://proxy2:8080"},
	}
	config := DefaultProxyPoolConfig()
	pool := NewEnhancedProxyPool(nil, config)
	pool.pool.AddProxy("http://proxy1:8080", 10)
	pool.pool.AddProxy("http://proxy2:8080", 10)
	pool.initialized.Store(true)
	pool.enabled.Store(true)
	for i := 0; i < 3; i++ {
		pool.MarkProxyFailure("http://proxy1:8080")
	}

	integration := &StoreProxyPoolIntegration{
		store:        store,
		enhancedPool: pool,
	}
	integration.useEnhanced.Store(true)
	store.proxyPoolIntegration = integration

	got := store.AcquireProxy("http://proxy1:8080")
	if got != "http://proxy2:8080" {
		t.Fatalf("AcquireProxy() = %q, want %q", got, "http://proxy2:8080")
	}
	store.ReleaseProxy(got)
}
