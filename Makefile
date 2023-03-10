K8S_VERSION := 1.24.2
arch        := amd64
os          := $(shell uname -s | tr '[:upper:]' '[:lower:]')
testbin_dir := ./.testbin/
tools_archive := kubebuilder-tools-${K8S_VERSION}-$(os)-$(arch).tar.gz

elector:
	go build -o bin/elector cmd/elector/*.go

test:
	go test ./... -count=1 -coverprofile cover.out -short

integration_test: kubebuilder
	go test ./pkg/election/... -tags=integration -v -count=1

kubebuilder: $(testbin_dir)/$(tools_archive)
	tar -xzf $(testbin_dir)/$(tools_archive) --strip-components=2 -C $(testbin_dir)
	chmod -R +x $(testbin_dir)

$(testbin_dir)/$(tools_archive):
	mkdir -p $(testbin_dir)
	curl -L -O --output-dir $(testbin_dir) "https://storage.googleapis.com/kubebuilder-tools/$(tools_archive)"
