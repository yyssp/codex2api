package proxy

import (
	"net/http"
	"testing"

	"github.com/codex2api/auth"
	"github.com/codex2api/database"
)

func newModelScopeStore() *auth.Store {
	return auth.NewStore(nil, nil, &database.SystemSettings{
		MaxConcurrency:  2,
		TestConcurrency: 1,
		TestModel:       "gpt-5.4",
	})
}

func TestApplyCooldownScopes404ToModel(t *testing.T) {
	store := newModelScopeStore()
	handler := &Handler{store: store}
	account := &auth.Account{DBID: 1, AccessToken: "tok-1", PlanType: "plus"}

	keepAccountFailure := handler.applyCooldown(account, "gpt-5.4", http.StatusNotFound, []byte(`{"error":{"message":"model gpt-5.4 not found"}}`), nil)

	if keepAccountFailure {
		t.Fatal("404 model failure should not keep account-wide penalty")
	}
	if account.IsModelAvailable("gpt-5.4") {
		t.Fatal("requested model should be marked unavailable")
	}
	if !account.IsModelAvailable("gpt-5.4-mini") {
		t.Fatal("other models should remain available")
	}
	if !account.IsAvailable() {
		t.Fatal("account should remain globally available")
	}
}

func TestApplyCooldownScopesGeneric429ToModel(t *testing.T) {
	store := newModelScopeStore()
	handler := &Handler{store: store}
	account := &auth.Account{DBID: 2, AccessToken: "tok-2", PlanType: "plus"}
	resp := &http.Response{Header: make(http.Header)}

	keepAccountFailure := handler.applyCooldown(account, "gpt-5.4", http.StatusTooManyRequests, []byte(`{"error":{"type":"rate_limit_error","message":"Too many requests"}}`), resp)

	if keepAccountFailure {
		t.Fatal("generic 429 should be scoped to the requested model")
	}
	if account.IsModelAvailable("gpt-5.4") {
		t.Fatal("requested model should be cooling down")
	}
	if !account.IsModelAvailable("gpt-5.4-mini") {
		t.Fatal("other models should remain available")
	}
	if account.HasActiveCooldown() {
		t.Fatal("account should not enter account-wide cooldown")
	}
}

func TestApplyCooldownPremium429KeepsAccountWideState(t *testing.T) {
	store := newModelScopeStore()
	handler := &Handler{store: store}
	account := &auth.Account{DBID: 3, AccessToken: "tok-3", PlanType: "plus"}
	resp := &http.Response{Header: make(http.Header)}
	resp.Header.Set("x-codex-primary-used-percent", "100")
	resp.Header.Set("x-codex-primary-window-minutes", "300")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "600")

	keepAccountFailure := handler.applyCooldown(account, "gpt-5.4", http.StatusTooManyRequests, []byte(`{"error":{"type":"usage_limit_reached"}}`), resp)

	if keepAccountFailure {
		t.Fatal("premium 5h path should be handled by cooldown logic directly")
	}
	if !account.IsPremium5hRateLimited() {
		t.Fatal("account should enter premium 5h rate limit state")
	}
	if !account.IsModelAvailable("gpt-5.4") {
		t.Fatal("premium 5h quota is account-wide, model state should not be separately poisoned")
	}
}

func TestApplyCooldownMissingScopeDoesNotPunishAccount(t *testing.T) {
	store := newModelScopeStore()
	handler := &Handler{store: store}
	account := &auth.Account{DBID: 4, AccessToken: "tok-4", PlanType: "plus"}

	keepAccountFailure := handler.applyCooldown(account, "gpt-5.4", http.StatusUnauthorized, []byte(`{"error":{"message":"Missing required scope","code":"missing_scope"}}`), nil)

	if keepAccountFailure {
		t.Fatal("missing_scope 401 should not keep account-wide penalty")
	}
	if !account.IsAvailable() {
		t.Fatal("account should remain available for missing_scope")
	}
}
