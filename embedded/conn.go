package embedded

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// unaryHandler is one registered unary method and the server that answers it.
type unaryHandler struct {
	server  any
	handler grpc.MethodHandler
}

// localConn is a gRPC connection that calls service implementations in the
// same process. Requests and responses are copied, never serialized.
type localConn struct {
	handlers map[string]unaryHandler
}

// newLocalConn returns a connection with no registered services.
func newLocalConn() *localConn {
	return &localConn{
		handlers: make(map[string]unaryHandler),
	}
}

// register routes the unary methods of a service to its implementation.
func (c *localConn) register(desc *grpc.ServiceDesc, server any) {
	for _, method := range desc.Methods {
		name := fmt.Sprintf("/%s/%s", desc.ServiceName,
			method.MethodName)
		c.handlers[name] = unaryHandler{
			server:  server,
			handler: method.Handler,
		}
	}
}

// Invoke calls the handler registered for method. Errors reach the caller as
// gRPC statuses, as they would over a network connection.
func (c *localConn) Invoke(ctx context.Context, method string, args,
	reply any, _ ...grpc.CallOption) error {

	registered, ok := c.handlers[method]
	if !ok {
		return status.Errorf(codes.Unimplemented, "method %s is "+
			"not implemented by the embedded backend", method)
	}

	request, ok := args.(proto.Message)
	if !ok {
		return status.Errorf(codes.Internal, "request of %s is not "+
			"a proto message", method)
	}
	response, ok := reply.(proto.Message)
	if !ok {
		return status.Errorf(codes.Internal, "response of %s is "+
			"not a proto message", method)
	}

	decode := func(target any) error {
		message, ok := target.(proto.Message)
		if !ok {
			return fmt.Errorf("request target is not a proto " +
				"message")
		}
		proto.Merge(message, request)

		return nil
	}

	result, err := registered.handler(registered.server, ctx, decode, nil)
	if err != nil {
		return toStatusError(ctx, err)
	}

	message, ok := result.(proto.Message)
	if !ok {
		return status.Errorf(codes.Internal, "handler of %s "+
			"returned no proto message", method)
	}
	proto.Merge(response, message)

	return nil
}

// NewStream rejects streaming calls, which the engine does not serve.
func (c *localConn) NewStream(_ context.Context, _ *grpc.StreamDesc,
	method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {

	return nil, status.Errorf(codes.Unimplemented, "stream %s is not "+
		"implemented by the embedded backend", method)
}

// toStatusError maps a handler error to the status a gRPC client would see.
func toStatusError(ctx context.Context, err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return status.FromContextError(ctxErr).Err()
	}

	return status.Error(codes.Unknown, err.Error())
}
