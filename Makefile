.PHONY: test compile doctor industrial-build industrial-demo runtime-fetch dev-up dev-down dev-status dev-smoke

test:
	go test ./...

compile:
	go run ./cmd/continuity compile -workflow examples/rescue.workflow.json -capabilities examples/capabilities

doctor:
	go run ./cmd/continuity doctor -root .

industrial-build:
	bash engines/build-industrial.sh

industrial-demo:
	bash ops/industrial/opcua-demo/run.sh

runtime-fetch:
	bash engines/RUN_RUNTIME_BINARIES.command

dev-up:
	bash ops/dev/start-local.sh

dev-down:
	bash ops/dev/stop-local.sh

dev-status:
	bash ops/dev/status-local.sh

dev-smoke:
	bash ops/dev/test-real-engines.sh
