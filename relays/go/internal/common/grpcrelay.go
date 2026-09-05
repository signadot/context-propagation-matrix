package common

import (
	"context"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
)

// GRPCRelay implements cpmatrix.Relay/Handle, delegating to Forward with the
// handler context (which carries the tool's extracted trace context, put
// there by the tool's server interceptor / stats handler).
type GRPCRelay struct {
	cpmatrix.UnimplementedRelayServer
	Forward ForwardFunc
}

// Handle implements cpmatrix.RelayServer.
func (s *GRPCRelay) Handle(ctx context.Context, req *cpmatrix.RelayRequest) (*cpmatrix.RelayResponse, error) {
	if err := s.Forward(ctx, req.GetTestId(), req.GetForwardVia()); err != nil {
		return nil, err
	}
	return &cpmatrix.RelayResponse{Status: "forwarded"}, nil
}
