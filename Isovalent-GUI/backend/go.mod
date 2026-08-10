module github.com/isovalent-control/isovalent-control/backend

go 1.24.0

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/golang-jwt/jwt/v5 v5.2.2
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.10.9
	google.golang.org/grpc v1.67.3
	google.golang.org/protobuf v1.36.5
)

require (
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142 // indirect
)

replace google.golang.org/grpc => github.com/grpc/grpc-go v1.67.3

replace google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.36.5

replace golang.org/x/net => github.com/golang/net v0.33.0

replace golang.org/x/sys => github.com/golang/sys v0.28.0

replace golang.org/x/text => github.com/golang/text v0.21.0

replace google.golang.org/genproto/googleapis/rpc => github.com/googleapis/go-genproto/googleapis/rpc v0.0.0-20240814211410-ddb44dafa142
