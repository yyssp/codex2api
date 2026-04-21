package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex2api/cache"
)

func TestStartUnauthorizedRecoveryInvalidatesAccessTokenAndRefreshes(t *testing.T) {
	tokenCache := cache.NewMemory(1)
	defer tokenCache.Close()

	store := &Store{
		tokenCache:     tokenCache,
		maxConcurrency: 2,
	}
	acc := &Account{
		DBID:               1,
		RefreshToken:       "rt-1",
		AccessToken:        "stale-at",
		ExpiresAt:          time.Now().Add(30 * time.Minute),
		Status:             StatusReady,
		HealthTier:         HealthTierBanned,
		LastUnauthorizedAt: time.Now(),
	}
	if err := tokenCache.SetAccessToken(context.Background(), acc.DBID, acc.AccessToken, time.Hour); err != nil {
		t.Fatalf("SetAccessToken() error = %v", err)
	}

	refreshed := make(chan struct{})
	store.refreshAccountOverride = func(ctx context.Context, target *Account) error {
		if target != acc {
			t.Fatalf("target account mismatch")
		}
		cached, err := tokenCache.GetAccessToken(ctx, acc.DBID)
		if err != nil {
			t.Fatalf("GetAccessToken() error = %v", err)
		}
		if cached != "" {
			t.Fatalf("cached token = %q, want empty after invalidation", cached)
		}

		target.mu.Lock()
		target.AccessToken = "fresh-at"
		target.ExpiresAt = time.Now().Add(time.Hour)
		target.Status = StatusReady
		target.HealthTier = HealthTierWarm
		target.recomputeSchedulerLocked(atomic.LoadInt64(&store.maxConcurrency))
		target.mu.Unlock()
		close(refreshed)
		return nil
	}

	if started := store.StartUnauthorizedRecovery(acc); !started {
		t.Fatal("StartUnauthorizedRecovery() = false, want true")
	}

	acc.mu.RLock()
	if acc.AccessToken != "" {
		t.Fatalf("AccessToken = %q, want empty during recovery", acc.AccessToken)
	}
	if acc.HealthTier != HealthTierRisky {
		t.Fatalf("HealthTier = %q, want %q after invalidation", acc.HealthTier, HealthTierRisky)
	}
	acc.mu.RUnlock()
	if got := atomic.LoadInt32(&acc.Disabled); got != 1 {
		t.Fatalf("Disabled = %d, want 1 during recovery", got)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery refresh")
	}

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&acc.Disabled) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&acc.Disabled); got != 0 {
		t.Fatalf("Disabled = %d, want 0 after successful refresh", got)
	}

	acc.mu.RLock()
	defer acc.mu.RUnlock()
	if acc.AccessToken != "fresh-at" {
		t.Fatalf("AccessToken = %q, want fresh-at", acc.AccessToken)
	}
	if acc.HealthTier != HealthTierRisky {
		t.Fatalf("HealthTier = %q, want %q after refresh", acc.HealthTier, HealthTierRisky)
	}
}

func TestStartUnauthorizedRecoveryReturnsFalseWithoutRefreshToken(t *testing.T) {
	store := &Store{maxConcurrency: 1}
	acc := &Account{
		DBID:        2,
		AccessToken: "at-only",
		Status:      StatusReady,
	}

	if started := store.StartUnauthorizedRecovery(acc); started {
		t.Fatal("StartUnauthorizedRecovery() = true, want false for AT-only account")
	}
	if got := atomic.LoadInt32(&acc.Disabled); got != 0 {
		t.Fatalf("Disabled = %d, want 0", got)
	}
}
