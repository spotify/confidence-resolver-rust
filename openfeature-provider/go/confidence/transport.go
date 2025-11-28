package confidence

import (
	"net/http"

	"google.golang.org/grpc"
)

// TransportHooks allows advanced customization of gRPC and HTTP networking.
// Implementations can override gRPC dialing (e.g., plaintext, interceptors, rerouting)
// and wrap the HTTP transport.
type TransportHooks interface {
	ModifyGRPCDial(target string, base []grpc.DialOption) (string, []grpc.DialOption)
	WrapHTTP(base http.RoundTripper) http.RoundTripper
}

type defaultTransportHooks struct{}

func (defaultTransportHooks) ModifyGRPCDial(target string, base []grpc.DialOption) (string, []grpc.DialOption) {
	return target, base
}

func (defaultTransportHooks) WrapHTTP(base http.RoundTripper) http.RoundTripper {
	return base
}

// DefaultTransportHooks is the library's default implementation used when no hooks are provided.
var DefaultTransportHooks TransportHooks = defaultTransportHooks{}
