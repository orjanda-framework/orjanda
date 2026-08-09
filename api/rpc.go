package api

import "github.com/orjanda-framework/orjanda/api/rpc"

type MethodHandler = rpc.MethodHandler
type MethodOpts = rpc.MethodOpts

// RegisterMethod registers a custom RPC method in the global RPC registry per PRD §14.3 / TAD §9.2.
func RegisterMethod(name string, h MethodHandler, opts MethodOpts) {
	rpc.RegisterMethod(name, h, opts)
}
