K8S_VERSION := 1.29
SHELL := /bin/bash

elector:
	go build -o bin/elector cmd/elector/*.go

.ONESHELL:
test: .envtest
	source .envtest
	go run github.com/onsi/ginkgo/v2/ginkgo run pkg/...

.envtest:
	go run sigs.k8s.io/controller-runtime/tools/setup-envtest use -p env $(K8S_VERSION) > .envtest
