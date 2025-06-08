module github.com/effectus/effectus/examples/buf_integration

go 1.21

replace github.com/effectus/effectus-go => ../../effectus-go

require (
	github.com/effectus/effectus-go v0.0.0-00010101000000-000000000000
	gopkg.in/yaml.v3 v3.0.1
)

require (
	google.golang.org/grpc v1.58.3 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
) 