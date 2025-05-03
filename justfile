base_dir := `pwd`

build:
	cd effectus-go && go build ./...

test:
	cd effectus-go && go test ./...

effectusc-build:
	cd effectus-go/cmd/effectusc && go build -o {{base_dir}}/bin/effectusc

effectusc-compile +files: effectusc-build
	./bin/effectusc -compile {{files}}

effectusc-run +files: 
	cd effectus-go/cmd/effectusc && go run main.go -verbose -compile {{files}}

test-standard-mill: 
	cd effectus-go/cmd/effectusc && go run main.go -verbose -schema {{base_dir}}/examples/schemas/manufacturing.json -verbschema {{base_dir}}/examples/schemas/manufacturing_verbs.json -compile {{base_dir}}/examples/flow/standard_mill.effx

test-mixed-process: 
	cd effectus-go/cmd/effectusc && go run main.go -verbose -schema {{base_dir}}/examples/schemas/manufacturing.json -verbschema {{base_dir}}/examples/schemas/manufacturing_verbs.json -compile {{base_dir}}/examples/flow/mixed_process.effx {{base_dir}}/examples/list/quality_check.eff

test-flow-parse: 
	cd effectus-go/cmd/test && go run main.go -mode=parse -ast -verbose {{base_dir}}/examples/test_flow.effx {{base_dir}}/examples/test.eff

test-flow-execute: 
	cd effectus-go/cmd/test && go run main.go -mode=run -execute -verbose {{base_dir}}/examples/test_flow.effx {{base_dir}}/examples/test.eff