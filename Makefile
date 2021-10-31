compile:
	protoc api/v1/*.proto \
			--go_out=. \
			--go_opt=paths=source_relative \
			--proto_path=. \
			--go-grpc_out=. \
			--go-grpc_opt=paths=source_relative

test:
	go test -race ./...