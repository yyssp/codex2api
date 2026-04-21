package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func int64Ptr(v int64) *int64 {
	return &v
}

func recomputeTestAccount(acc *Account, baseLimit int64) {
	acc.mu.Lock()
	acc.recomputeSchedulerLocked(baseLimit)
	acc.mu.Unlock()
}

func TestAccountPremiumPlanGetsDefaultScoreBias(t *testing.T) {
	acc := &Account{
		AccessToken: "token",
		Status:      StatusReady,
		PlanType:    "plus",
	}

	recomputeTestAccount(acc, 6)

	if acc.SchedulerScore != 100 {
		t.Fatalf("SchedulerScore = %v, want 100", acc.SchedulerScore)
	}
	if acc.DispatchScore != 150 {
		t.Fatalf("DispatchScore = %v, want 150", acc.DispatchScore)
	}
	if acc.ScoreBiasEffective != 50 {
		t.Fatalf("ScoreBiasEffective = %d, want 50", acc.ScoreBiasEffective)
	}
	if acc.BaseConcurrencyEffective != 6 {
		t.Fatalf("BaseConcurrencyEffective = %d, want 6", acc.BaseConcurrencyEffective)
	}
}

func TestAccountScoreBiasOverrideReplacesPlanDefault(t *testing.T) {
	acc := &Account{
		AccessToken:       "token",
		Status:            StatusReady,
		PlanType:          "team",
		ScoreBiasOverride: int64Ptr(12),
	}

	recomputeTestAccount(acc, 6)

	if acc.DispatchScore != 112 {
		t.Fatalf("DispatchScore = %v, want 112", acc.DispatchScore)
	}
	if acc.ScoreBiasEffective != 12 {
		t.Fatalf("ScoreBiasEffective = %d, want 12", acc.ScoreBiasEffective)
	}
}

func TestAccountRiskyTierDoesNotApplyScoreBias(t *testing.T) {
	acc := &Account{
		AccessToken:        "token",
		Status:             StatusReady,
		PlanType:           "pro",
		LastUnauthorizedAt: time.Now(),
	}

	recomputeTestAccount(acc, 6)

	if acc.HealthTier != HealthTierRisky {
		t.Fatalf("HealthTier = %s, want %s", acc.HealthTier, HealthTierRisky)
	}
	if acc.SchedulerScore >= 60 {
		t.Fatalf("SchedulerScore = %v, want < 60", acc.SchedulerScore)
	}
	if acc.DispatchScore != acc.SchedulerScore {
		t.Fatalf("DispatchScore = %v, want raw score %v when risky", acc.DispatchScore, acc.SchedulerScore)
	}
	if acc.ScoreBiasEffective != 0 {
		t.Fatalf("ScoreBiasEffective = %d, want 0", acc.ScoreBiasEffective)
	}
}

func TestAccountBaseConcurrencyOverrideControlsDynamicLimit(t *testing.T) {
	acc := &Account{
		AccessToken:             "token",
		Status:                  StatusReady,
		PlanType:                "plus",
		BaseConcurrencyOverride: int64Ptr(4),
	}

	recomputeTestAccount(acc, 10)
	if acc.DynamicConcurrencyLimit != 4 {
		t.Fatalf("healthy DynamicConcurrencyLimit = %d, want 4", acc.DynamicConcurrencyLimit)
	}

	acc.mu.Lock()
	acc.LastFailureAt = time.Now()
	acc.mu.Unlock()
	recomputeTestAccount(acc, 10)
	if acc.HealthTier != HealthTierWarm {
		t.Fatalf("warm HealthTier = %s, want %s", acc.HealthTier, HealthTierWarm)
	}
	if acc.DynamicConcurrencyLimit != 2 {
		t.Fatalf("warm DynamicConcurrencyLimit = %d, want 2", acc.DynamicConcurrencyLimit)
	}

	acc.mu.Lock()
	acc.LastUnauthorizedAt = time.Now()
	acc.mu.Unlock()
	recomputeTestAccount(acc, 10)
	if acc.HealthTier != HealthTierRisky {
		t.Fatalf("risky HealthTier = %s, want %s", acc.HealthTier, HealthTierRisky)
	}
	if acc.DynamicConcurrencyLimit != 1 {
		t.Fatalf("risky DynamicConcurrencyLimit = %d, want 1", acc.DynamicConcurrencyLimit)
	}
}

func TestNeedsUsageProbeSkipsRateLimited(t *testing.T) {
	acc := &Account{
		AccessToken:    "token",
		Status:         StatusCooldown,
		CooldownReason: "rate_limited",
	}
	if acc.NeedsUsageProbe(10 * time.Minute) {
		t.Fatal("NeedsUsageProbe should return false for rate_limited cooldown")
	}
}

func TestNeedsUsageProbeSkipsUnauthorized(t *testing.T) {
	acc := &Account{
		AccessToken:    "token",
		Status:         StatusCooldown,
		CooldownReason: "unauthorized",
	}
	if acc.NeedsUsageProbe(10 * time.Minute) {
		t.Fatal("NeedsUsageProbe should return false for unauthorized cooldown")
	}
}

func TestNeedsUsageProbeAllowsReadyAccount(t *testing.T) {
	acc := &Account{
		AccessToken: "token",
		Status:      StatusReady,
	}
	// UsagePercent7dValid = false，应该返回 true
	if !acc.NeedsUsageProbe(10 * time.Minute) {
		t.Fatal("NeedsUsageProbe should return true for ready account without valid usage data")
	}
}

func TestStoreNextPrefersHigherDispatchScoreWithinTier(t *testing.T) {
	premium := &Account{
		DBID:        1,
		AccessToken: "token",
		Status:      StatusReady,
		PlanType:    "pro",
	}
	regular := &Account{
		DBID:        2,
		AccessToken: "token",
		Status:      StatusReady,
		PlanType:    "free",
	}
	recomputeTestAccount(premium, 2)
	recomputeTestAccount(regular, 2)

	store := &Store{
		accounts: []*Account{regular, premium},
	}
	store.SetMaxConcurrency(2)

	got := store.Next()
	if got == nil {
		t.Fatal("Next() returned nil")
	}
	defer store.Release(got)

	if got.DBID != premium.DBID {
		t.Fatalf("Next() picked dbID=%d, want premium account %d", got.DBID, premium.DBID)
	}
}

func TestATOnlyExpiredAccountIsUnavailableAndRuntimeStatusError(t *testing.T) {
	acc := &Account{
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(-time.Minute),
		Status:      StatusReady,
	}

	if acc.IsAvailable() {
		t.Fatal("IsAvailable() = true, want false for expired AT-only account")
	}
	if got := acc.RuntimeStatus(); got != "error" {
		t.Fatalf("RuntimeStatus() = %q, want %q", got, "error")
	}
	if acc.NeedsRefresh() {
		t.Fatal("NeedsRefresh() = true, want false for AT-only account without RT")
	}
}

func TestExpiredRTBackedAccountCanStillBeScheduledForRefreshFlow(t *testing.T) {
	acc := &Account{
		RefreshToken: "rt",
		AccessToken:  "token",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Status:       StatusReady,
	}

	if !acc.IsAvailable() {
		t.Fatal("IsAvailable() = false, want true for RT-backed account")
	}
	if got := acc.RuntimeStatus(); got != "active" {
		t.Fatalf("RuntimeStatus() = %q, want %q", got, "active")
	}
	if !acc.NeedsRefresh() {
		t.Fatal("NeedsRefresh() = false, want true for RT-backed expired token")
	}
}

func TestStoreNextWithoutFastSchedulerRespectsConcurrencyLimitUnderContention(t *testing.T) {
	acc := &Account{
		DBID:        1,
		AccessToken: "token",
		Status:      StatusReady,
	}
	store := &Store{
		accounts:       []*Account{acc},
		maxConcurrency: 1,
	}

	const workers = 64
	start := make(chan struct{})
	results := make(chan *Account, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- store.Next()
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for picked := range results {
		if picked != nil {
			successes++
		}
	}

	if successes != 1 {
		t.Fatalf("successful acquisitions = %d, want 1", successes)
	}
	if got := atomic.LoadInt64(&acc.ActiveRequests); got != 1 {
		t.Fatalf("ActiveRequests = %d, want 1", got)
	}

	store.Release(acc)
}
