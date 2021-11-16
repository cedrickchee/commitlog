# Stage 1: builds our service
FROM golang:1.17.3-alpine AS build

WORKDIR /go/src/commitlog
COPY . .
# We must statically compile our binaries for them to run in the scratch image
# because it doesn't contain the system libraries needed to run dynamically
# linked binaries. That's why we disable Cgo—the compiler links it dynamically.
RUN CGO_ENABLED=0 go build -o /go/bin/commitlog ./cmd/commitlog
# download the grpc_health_probe executable.
RUN GRPC_HEALTH_PROBE_VERSION=v0.4.6 && \
    wget -qO/go/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/\
    ${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /go/bin/grpc_health_probe

# Stage 2: runs it

# scratch is empty image--the smallest Docker image.
FROM scratch
# copy our binary into this image.
COPY --from=build /go/bin/commitlog /bin/commitlog
# install the grpc_health_probe executable in out image.
COPY --from=build /go/bin/grpc_health_probe /bin/grpc_health_probe

ENTRYPOINT ["/bin/commitlog"]