K8S_VERSION := 1.27.1
SHELL := /bin/bash

elector:
	go build -o bin/elector cmd/elector/*.go

test:
	go test ./... -count=1 -coverprofile cover.out -short

.ONESHELL:
integration_test:
	source <(go run sigs.k8s.io/controller-runtime/tools/setup-envtest use -p env $(K8S_VERSION))
	go test ./pkg/election/... -tags=integration -v -count=1
