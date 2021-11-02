ROOT_DIR:=$(strip $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST)))))

export CONFIG_DIR=${ROOT_DIR}/.config

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

.PHONY: compile
compile:
	protoc api/v1/*.proto \
			--go_out=. \
			--go_opt=paths=source_relative \
			--proto_path=. \
			--go-grpc_out=. \
			--go-grpc_opt=paths=source_relative

.PHONY: test
test:
	go test -race ./...