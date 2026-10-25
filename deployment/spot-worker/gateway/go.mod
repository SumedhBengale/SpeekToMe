module speektome/spot-worker-gateway

go 1.26.10

require (
	github.com/gorilla/websocket v1.5.3
	github.com/pion/opus v0.1.0
)

require (
	speektome/transport v0.0.0
	speektome/vocabulary v0.0.0
)

replace speektome/vocabulary => ../../../shared/vocabulary

replace speektome/transport => ../../../shared/transport
