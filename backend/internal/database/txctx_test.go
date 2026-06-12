package database

import (
	"context"
	"testing"
)

func TestAfterCommitRunsImmediatelyWithoutTransaction(t *testing.T) {
	called := false
	AfterCommit(context.Background(), func() { called = true })
	if !called {
		t.Fatal("expected callback to run immediately outside transaction")
	}
}

func TestAfterCommitDefersWithTransactionContext(t *testing.T) {
	ctx := ContextWithTx(context.Background(), nil)
	called := false

	AfterCommit(ctx, func() { called = true })
	if called {
		t.Fatal("expected callback to be deferred while inside transaction context")
	}

	state, ok := txStateFromContext(ctx)
	if !ok {
		t.Fatal("expected transaction state in context")
	}
	callbacks := state.drainAfterCommit()
	if len(callbacks) != 1 {
		t.Fatalf("expected 1 deferred callback, got %d", len(callbacks))
	}
	callbacks[0]()
	if !called {
		t.Fatal("expected deferred callback to execute")
	}
}

func TestAfterRollbackDefersWithTransactionContext(t *testing.T) {
	ctx := ContextWithTx(context.Background(), nil)
	called := false

	AfterRollback(ctx, func() { called = true })
	if called {
		t.Fatal("expected rollback callback to be deferred")
	}

	state, ok := txStateFromContext(ctx)
	if !ok {
		t.Fatal("expected transaction state in context")
	}
	callbacks := state.drainAfterRollback()
	if len(callbacks) != 1 {
		t.Fatalf("expected 1 rollback callback, got %d", len(callbacks))
	}
	callbacks[0]()
	if !called {
		t.Fatal("expected rollback callback to execute")
	}
}
