package a2a

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// bearerAuthInterceptor enforces Authorization: Bearer <token> on inbound A2A calls.
type bearerAuthInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	token string
}

func (b *bearerAuthInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	vals, ok := callCtx.ServiceParams().Get("authorization")
	if !ok || len(vals) == 0 {
		vals, ok = callCtx.ServiceParams().Get("Authorization")
	}
	if !ok || len(vals) == 0 {
		return ctx, nil, fmt.Errorf("missing Authorization bearer token")
	}
	got := strings.TrimSpace(vals[0])
	want := "Bearer " + b.token
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return ctx, nil, fmt.Errorf("invalid bearer token")
	}
	return ctx, nil, nil
}
