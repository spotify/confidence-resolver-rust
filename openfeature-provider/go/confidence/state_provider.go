package confidence

import "context"

// StateProvider is an interface for providing resolver state and account ID
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, error)
	GetAccountID() string
}
