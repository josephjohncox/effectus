base_dir := `pwd`

build:
	cd effectus-go && go build ./...

test:
	cd effectus-go && go test ./...

effectusc-build:
	cd effectus-go/cmd/effectusc && go build -o {{base_dir}}/bin/effectusc

effectusc-flow file:
	just effectusc-build
	./bin/effectusc flow {{file}}

effectusc-list file:
	just effectusc-build
	./bin/effectusc list {{file}}

test-standard-mill:
	just effectusc-flow examples/flow/standard_mill.effx

test-mixed-process:
	just effectusc-flow examples/flow/mixed_process.effx 

test-flow-parse: 
	cd effectus-go/ && go build -o {{base_dir}}/bin/test cmd/test/main.go
	./bin/test -mode=parse -verbose {{base_dir}}/examples/test_flow.effx

test-flow-execute: 
	cd effectus-go/ && go build -o {{base_dir}}/bin/test cmd/test/main.go
	./bin/test -mode=run -execute -verbose {{base_dir}}/examples/test_flow.effx