package confidence

import (
	"context"

	messages "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
)

type LocalResolverSupplier func() LocalResolver

type LocalResolverFactory interface {
	New() LocalResolver
	Close(context.Context) error
}
type LocalResolver interface {
	SetResolverState(*messages.SetResolverStateRequest) error
	ResolveWithSticky(*resolver.ResolveWithStickyRequest) (*resolver.ResolveWithStickyResponse, error)
	FlushAllLogs() error
	FlushAssignLogs() error
	Close(context.Context) error
}
