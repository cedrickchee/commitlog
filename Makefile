ROOT_DIR:=$(strip $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST)))))

export CONFIG_DIR=${ROOT_DIR}/.config

################################################################################
# Develop
################################################################################
.PHONY: init
init:
	mkdir -p ${CONFIG_DIR}

.PHONY: gencert
gencert:
	# Generating self-signed root CA certificate and private key
	cfssl gencert \
			-initca test/ca-csr.json | cfssljson -bare ca
	
	# Generating self-signed server certificate and private key
	cfssl gencert \
			-ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=test/ca-config.json \
			-profile=server \
			test/server-csr.json | cfssljson -bare server

	# Generating multiple client certs and private keys
	cfssl gencert \
			-ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=test/ca-config.json \
			-profile=client \
			-cn="root" \
			test/client-csr.json | cfssljson -bare root-client
	cfssl gencert \
			-ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=test/ca-config.json \
			-profile=client \
			-cn="nobody" \
			test/client-csr.json | cfssljson -bare nobody-client

	mv *.pem *.csr ${CONFIG_DIR}

################################################################################
# Build
################################################################################
.PHONY: compile
compile:
	protoc api/v1/*.proto \
			--go_out=. \
			--go_opt=paths=source_relative \
			--proto_path=. \
			--go-grpc_out=. \
			--go-grpc_opt=paths=source_relative

# Build Docker image
TAG ?= 0.0.1

.PHONY: build-docker
build-docker:
	docker build -t github.com/cedrickchee/commitlog:$(TAG) .

################################################################################
# Test
################################################################################
.PHONY: test
test: "${CONFIG_DIR}/policy.csv" "${CONFIG_DIR}/model.conf"
	go test -race ./...

#
# Install the model file into the CONFIG_PATH so tests can find them
#
"${CONFIG_DIR}/model.conf":
	cp test/model.conf "${CONFIG_DIR}/model.conf"

"${CONFIG_DIR}/policy.csv":
	cp test/policy.csv "${CONFIG_DIR}/policy.csv"

.PHONY: testserver
testserver: "${CONFIG_DIR}/policy.csv" "${CONFIG_DIR}/model.conf"	
	go test -v -race ./internal/server -debug=true

.PHONY: testsvcdisco
testsvcdisco: "${CONFIG_DIR}/policy.csv" "${CONFIG_DIR}/model.conf"	
	go test -v -race ./internal/discovery

# Test the distributed log and Raft integrated in our service
.PHONY: testraft
testraft: "${CONFIG_DIR}/policy.csv" "${CONFIG_DIR}/model.conf"	
	go test -v -race ./internal/log/distributed_test.go

# Test distributed service end-to-end (uses Raft for consensus and log replication)
.PHONY: testagent
testagent: "${CONFIG_DIR}/policy.csv" "${CONFIG_DIR}/model.conf"	
	go test -v -race ./internal/agent/agent_test.go

# Run the load balance's resolver tests
.PHONY: testresolver
testresolver:
	go test -v -race ./internal/loadbalance/resolver_test.go

# Run the load balance's picker tests
.PHONY: testpicker
testpicker:
	go test -v -race ./internal/loadbalance/picker_test.go

################################################################################
# Deploy
################################################################################
.PHONY: deploy-local-kind
deploy-local-kind:
	# Create a Kind cluster
	kind create cluster

	# Build Docker image
	make build-docker
	
	# Load Docker image into our Kind cluster
	kind load docker-image github.com/cedrickchee/commitlog:0.0.1

	make local-install-chart
	make local-list-pods

.PHONY: local-install-chart
local-install-chart:
	# Copy the ACL files into Helm charts
	cp test/model.conf deploy/commitlog/.config/model.conf
	cp test/policy.csv deploy/commitlog/.config/policy.csv

	# Install our Helm chart in our Kind cluster
	helm install commitlog deploy/commitlog

# List Kubernetes pods
.PHONY: local-list-pods
local-list-pods:
	kubectl get pods

.PHONY: forward-port-local
forward-port-local:
	kubectl port-forward pod/commitlog-0 8500:8400

# View local cluster pod's logs
.PHONY: local-pod-logs
local-pod-logs:
	kubectl logs -f commitlog-0

.PHONY: local-pod-exec
local-pod-exec:
	kubectl exec --stdin --tty commitlog-0 -c commitlog -- sh

.PHONY: uninstall-chart
uninstall-chart:
	helm uninstall commitlog

.PHONY: delete-local-cluster
delete-local-cluster:
	kind delete cluster

.PHONY: cleanup-local-cluster
cleanup-local-cluster:
	make uninstall-chart
	make delete-local-cluster