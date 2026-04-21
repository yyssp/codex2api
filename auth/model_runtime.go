package auth

import (
	"strings"
	"time"
)

const defaultModelUnavailableTTL = 12 * time.Hour

type ModelRuntimeState struct {
	Model            string
	Reason           string
	LastStatusCode   int
	CooldownUntil    time.Time
	UnavailableUntil time.Time
	LastFailureAt    time.Time
	LastSuccessAt    time.Time
}

func normalizeModelKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func (a *Account) lookupModelStateLocked(model string) (string, *ModelRuntimeState) {
	key := normalizeModelKey(model)
	if key == "" || len(a.ModelStates) == 0 {
		return key, nil
	}
	return key, a.ModelStates[key]
}

func (a *Account) ensureModelStateLocked(model string) (string, *ModelRuntimeState) {
	key := normalizeModelKey(model)
	if key == "" {
		return "", nil
	}
	if a.ModelStates == nil {
		a.ModelStates = make(map[string]*ModelRuntimeState)
	}
	state := a.ModelStates[key]
	if state == nil {
		state = &ModelRuntimeState{Model: key}
		a.ModelStates[key] = state
	}
	return key, state
}

func (a *Account) clearExpiredModelStateLocked(model string, now time.Time) {
	key, state := a.lookupModelStateLocked(model)
	if key == "" || state == nil {
		return
	}
	if state.CooldownUntil.After(now) || state.UnavailableUntil.After(now) {
		return
	}
	delete(a.ModelStates, key)
}

func (a *Account) modelAvailableLocked(model string, now time.Time) bool {
	key, state := a.lookupModelStateLocked(model)
	if key == "" || state == nil {
		return true
	}
	if state.CooldownUntil.After(now) || state.UnavailableUntil.After(now) {
		return false
	}
	delete(a.ModelStates, key)
	return true
}

func (a *Account) markModelCooldownLocked(model string, until time.Time, reason string, statusCode int) {
	key, state := a.ensureModelStateLocked(model)
	if key == "" || state == nil {
		return
	}
	if until.After(state.CooldownUntil) {
		state.CooldownUntil = until
	}
	state.Reason = reason
	state.LastStatusCode = statusCode
	state.LastFailureAt = time.Now()
}

func (a *Account) markModelUnavailableLocked(model string, until time.Time, reason string, statusCode int) {
	key, state := a.ensureModelStateLocked(model)
	if key == "" || state == nil {
		return
	}
	if until.After(state.UnavailableUntil) {
		state.UnavailableUntil = until
	}
	state.Reason = reason
	state.LastStatusCode = statusCode
	state.LastFailureAt = time.Now()
}

func (a *Account) clearModelStateLocked(model string) {
	key := normalizeModelKey(model)
	if key == "" || len(a.ModelStates) == 0 {
		return
	}
	delete(a.ModelStates, key)
}

func (a *Account) IsModelAvailable(model string) bool {
	if a == nil {
		return false
	}
	key := normalizeModelKey(model)
	if key == "" {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modelAvailableLocked(key, time.Now())
}
