package confidence

import (
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
)

type FlagLogger interface {
	Write(request *resolverv1.WriteFlagLogsRequest)
	Shutdown()
}
