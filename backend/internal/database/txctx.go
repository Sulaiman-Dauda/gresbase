package database

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
)

type txStateKey struct{}

type txState struct {
	tx            pgx.Tx
	mu            sync.Mutex
	afterCommit   []func()
	afterRollback []func()
}

// ContextWithTx returns a new context bound to tx.
func ContextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txStateKey{}, &txState{tx: tx})
}

func txStateFromContext(ctx context.Context) (*txState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(txStateKey{}).(*txState)
	return state, ok && state != nil
}

// TxFromContext returns the transaction bound to ctx, if any.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	state, ok := txStateFromContext(ctx)
	if !ok || state.tx == nil {
		return nil, false
	}
	return state.tx, true
}

// AfterCommit registers fn to be executed only after the surrounding
// transaction successfully commits. If ctx has no transaction, fn runs immediately.
func AfterCommit(ctx context.Context, fn func()) {
	if fn == nil {
		return
	}
	state, ok := txStateFromContext(ctx)
	if !ok {
		fn()
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.afterCommit = append(state.afterCommit, fn)
}

// AfterRollback registers fn to be executed if the surrounding transaction rolls back.
func AfterRollback(ctx context.Context, fn func()) {
	if fn == nil {
		return
	}
	state, ok := txStateFromContext(ctx)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.afterRollback = append(state.afterRollback, fn)
}

func (s *txState) drainAfterCommit() []func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	callbacks := append([]func(){}, s.afterCommit...)
	s.afterCommit = nil
	return callbacks
}

func (s *txState) drainAfterRollback() []func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	callbacks := append([]func(){}, s.afterRollback...)
	s.afterRollback = nil
	return callbacks
}
