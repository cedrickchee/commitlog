CONFIG_PATH=.config/

.PHONY: init
init:
	mkdir -p ${CONFIG_PATH}

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
	mv *.pem *.csr ${CONFIG_PATH}

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