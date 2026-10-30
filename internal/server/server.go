package server

import "github.com/google/wire"

// ProviderSet 将 HTTP 与 gRPC 放在同一应用生命周期内启动和关闭。
var ProviderSet = wire.NewSet(NewHTTPServer, NewGRPCServer)
