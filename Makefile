# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CLIENT_GEN ?= $(LOCALBIN)/client-gen
LISTER_GEN ?= $(LOCALBIN)/lister-gen
INFORMER_GEN ?= $(LOCALBIN)/informer-gen

# Path to the converter API
CONVERTER_API_PATH ?= github.com/Netcracker/qubership-grafana-operator-converter/api

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.20.1
CODEGENERATOR_VERSION ?= v0.36.0
GOLANGCI_LINT_VERSION ?= v2.12.1

# Current version of the converter
VERSION ?= 0.1.0

# Image URL to use all building/pushing image targets
DOCKERFILE ?= Dockerfile
REGISTRY ?= ghcr.io
ORG ?= netcracker
CONTAINER_NAME ?= $(REGISTRY)/$(ORG)/qubership-grafana-operator-converter:v$(VERSION)

###########
# Generic #
###########

# Default run without arguments
.PHONY: all
all: generate fmt vet test image

# Run unit tests in all packages
.PHONY: test
test: generate fmt vet
	go test ./... -coverprofile cover.out

#########
# Build #
#########

## Build manager binary
.PHONY: build
build: generate fmt vet
	go build -o bin/manager main.go

# Run go vet against code
.PHONY: vet
vet:
	go vet ./...

# Run go fmt against code
.PHONY: fmt
fmt:
	go fmt ./...

# Run golangci-lint against code
.PHONY: golangci-lint
golangci-lint: golangci
	$(GOLANGCI) run ./...

# Find or download golangci-lint
golangci:
ifeq (, $(shell which golangci-lint))
	@{ \
	set -e ;\
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) ;\
	}
GOLANGCI=$(GOBIN)/golangci-lint
else
GOLANGCI=$(shell which golangci-lint)
endif

###############
# Build image #
###############

.PHONY: image
image:
	echo "=> Build image ..."
	docker build --pull -t $(CONTAINER_NAME) -f $(DOCKERFILE) .

	# Set image tag if build inside the Jenkins
	for id in $(DOCKER_NAMES) ; do \
		docker tag $(CONTAINER_NAME) "$$id"; \
	done

###################
# Running locally #
###################

# Run against the configured Kubernetes cluster in ~/.kube/config
.PHONY: run
run: generate fmt vet
	go run ./main.go

##############
# Generating #
##############

# Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations
.PHONY: generate
generate: controller-gen generate-crds append-helm-hooks-crds

# Generate CRDs
.PHONY: generate-crds
generate-crds:
	$(CONTROLLER_GEN) crd:crdVersions={v1} \
					  paths="./api/operator/v1alpha1" \
					  output:artifacts:config=charts/qubership-grafana-operator-converter/crds/
	$(CONTROLLER_GEN) crd:crdVersions={v1} \
					  paths="./api/operator/v1beta1" \
					  output:artifacts:config=charts/qubership-grafana-operator-converter/crds/

# Append Helm hooks to CRDs
.PHONY: append-helm-hooks-crds
append-helm-hooks-crds:
	if [[ "$$OSTYPE" == "darwin"* ]]; then \
		SED_CMD="sed -i '' -e"; \
	else \
		SED_CMD="sed -i"; \
	fi; \
	find charts/qubership-grafana-operator-converter/crds -name '*.yaml' | while read f; do \
		$$SED_CMD "/^    controller-gen.kubebuilder.io.version.*/a\\    helm.sh/hook-weight: \"-5\"" "$$f"; \
		$$SED_CMD "/^    controller-gen.kubebuilder.io.version.*/a\\    helm.sh/hook: crd-install" "$$f"; \
	done

# Find or download controller-gen
# download controller-gen if necessary
.PHONY: controller-gen
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION) ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# Generate API clients for v1alpha1
.PHONY: api-gen-v1alpha1
api-gen-v1alpha1: client-gen lister-gen informer-gen
	rm -rf api/client/v1alpha1
	@echo ">> generating with client-gen"
	$(CLIENT_GEN) \
		--clientset-name versioned \
		--input-base "" \
		--input $(CONVERTER_API_PATH)/operator/v1alpha1 \
		--output-pkg $(CONVERTER_API_PATH)/client/v1alpha1/clientset \
		--output-dir ./api/client/v1alpha1/clientset \
		--v 10
	@echo ">> generating with lister-gen"
	$(LISTER_GEN) $(CONVERTER_API_PATH)/operator/v1alpha1 \
		--output-dir ./api/client/v1alpha1/listers \
		--output-pkg $(CONVERTER_API_PATH)/client/v1alpha1/listers \
		--v 10
	@echo ">> generating with informer-gen"
	$(INFORMER_GEN) $(CONVERTER_API_PATH)/operator/v1alpha1 \
		--versioned-clientset-package $(CONVERTER_API_PATH)/client/v1alpha1/clientset/versioned \
		--listers-package $(CONVERTER_API_PATH)/client/v1alpha1/listers \
		--output-dir ./api/client/v1alpha1/informers \
		--output-pkg $(CONVERTER_API_PATH)/client/v1alpha1/informers \
		--v 10

# Generate API clients for v1beta1
.PHONY: api-gen-v1beta1
api-gen-v1beta1: client-gen lister-gen informer-gen
	rm -rf api/client/v1beta1
	@echo ">> generating with client-gen"
	$(CLIENT_GEN) \
		--clientset-name versioned \
		--input-base "" \
		--input $(CONVERTER_API_PATH)/operator/v1beta1 \
		--output-pkg $(CONVERTER_API_PATH)/client/v1beta1/clientset \
		--output-dir ./api/client/v1beta1/clientset \
		--v 10
	@echo ">> generating with lister-gen"
	$(LISTER_GEN) $(CONVERTER_API_PATH)/operator/v1beta1 \
		--output-dir ./api/client/v1beta1/listers \
		--output-pkg $(CONVERTER_API_PATH)/client/v1beta1/listers \
		--v 10
	@echo ">> generating with informer-gen"
	$(INFORMER_GEN) $(CONVERTER_API_PATH)/operator/v1beta1 \
		--versioned-clientset-package $(CONVERTER_API_PATH)/client/v1beta1/clientset/versioned \
		--listers-package $(CONVERTER_API_PATH)/client/v1beta1/listers \
		--output-dir ./api/client/v1beta1/informers \
		--output-pkg $(CONVERTER_API_PATH)/client/v1beta1/informers \
		--v 10

# Generate API clients
.PHONY: client-gen
client-gen:
ifeq (, $(shell which client-gen))
	@{ \
	set -e ;\
	CLIENT_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CLIENT_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install k8s.io/code-generator/cmd/client-gen@$(CODEGENERATOR_VERSION) ;\
	rm -rf $$CLIENT_GEN_TMP_DIR ;\
	}
CLIENT_GEN=$(GOBIN)/client-gen
else
CLIENT_GEN=$(shell which client-gen)
endif

# Generate API clients
.PHONY: lister-gen
lister-gen:
ifeq (, $(shell which lister-gen))
	@{ \
	set -e ;\
	LISTER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$LISTER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install k8s.io/code-generator/cmd/lister-gen@$(CODEGENERATOR_VERSION) ;\
	rm -rf $$LISTER_GEN_TMP_DIR ;\
	}
LISTER_GEN=$(GOBIN)/lister-gen
else
LISTER_GEN=$(shell which lister-gen)
endif

# Generate API clients
.PHONY: informer-gen
informer-gen:
ifeq (, $(shell which informer-gen))
	@{ \
	set -e ;\
	INFORMER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$INFORMER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install k8s.io/code-generator/cmd/informer-gen@$(CODEGENERATOR_VERSION) ;\
	rm -rf $$INFORMER_GEN_TMP_DIR ;\
	}
INFORMER_GEN=$(GOBIN)/informer-gen
else
INFORMER_GEN=$(shell which informer-gen)
endif

# Display this help message
.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
