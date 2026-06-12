package api

import (
	"context"

	apimw "github.com/gresbase/gresbase/internal/api/middleware"
	"github.com/gresbase/gresbase/internal/database"
)

func cloneRequestContext(ctx context.Context, includeTx bool) context.Context {
	fresh := context.Background()
	if includeTx {
		if tx, ok := database.TxFromContext(ctx); ok {
			fresh = database.ContextWithTx(fresh, tx)
		}
	}

	keys := []any{
		apimw.CtxAdminID,
		apimw.CtxAdminRole,
		apimw.CtxAdminEmail,
		apimw.CtxRequestID,
		apimw.CtxAdminAuthMethod,
		apimw.CtxAPIKeyID,
		apimw.CtxAPIKeyPermissions,
		"record_id",
		"collection_id",
		"email",
		"verified",
	}
	for _, key := range keys {
		if val := ctx.Value(key); val != nil {
			fresh = context.WithValue(fresh, key, val)
		}
	}
	return fresh
}
