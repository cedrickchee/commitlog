# Stage 1: builds our service
FROM golang:1.17.3-alpine AS build

WORKDIR /go/src/commitlog
COPY . .
# We must statically compile our binaries for them to run in the scratch image
# because it doesn't contain the system libraries needed to run dynamically
# linked binaries. That's why we disable Cgo—the compiler links it dynamically.
RUN CGO_ENABLED=0 go build -o /go/bin/commitlog ./cmd/commitlog

# Stage 2: runs it

# scratch is empty image--the smallest Docker image.
FROM scratch
# copy our binary into this image.
COPY --from=build /go/bin/commitlog /bin/commitlog

ENTRYPOINT ["/bin/commitlog"]