module github.com/spotify/confidence-resolver-rust/openfeature-provider/go/bench

go 1.24.0

require (
	github.com/open-feature/go-sdk v1.16.0
	github.com/spotify/confidence-resolver/openfeature-provider/go v0.0.0
	google.golang.org/grpc v1.75.1
)

require (
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/tetratelabs/wazero v1.9.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/genproto v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251014184007-4626949a642f // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/spotify/confidence-resolver/openfeature-provider/go => ..
