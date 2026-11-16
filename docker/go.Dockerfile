# Builds the uninstrumented Go components (sink recorder + test driver).
#
# go.mod's `go` directive is the single source of truth for the Go version.
# The image is only a bootstrap: it ships with GOTOOLCHAIN=local, so when a
# Dependabot bump raises the directive past the image, `go mod download`
# refuses. `auto` lets `go` fetch the toolchain go.mod asks for instead.
# scripts/check-toolchains.sh still requires the image tag to be at or above
# the directive, so the download is a safety net, not the steady state.
FROM golang:1.27 AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
# The root module `replace`s the stubs module at ./gen, so its manifest must
# be in place before the download.
COPY go.mod go.sum ./
COPY gen/go.mod gen/go.sum ./gen/
RUN go mod download
COPY gen ./gen
# The driver's header evaluation and result types. Only these two packages:
# they are the ones internal/affected re-runs every variant for, and the
# build fails here if the driver or sink starts importing another.
COPY internal/eval ./internal/eval
COPY internal/raw ./internal/raw
COPY sink ./sink
COPY driver ./driver
RUN CGO_ENABLED=0 go build -o /out/sink ./sink \
 && CGO_ENABLED=0 go build -o /out/driver ./driver

FROM gcr.io/distroless/static-debian12 AS sink
COPY --from=build /out/sink /sink
ENTRYPOINT ["/sink"]

FROM gcr.io/distroless/static-debian12 AS driver
COPY --from=build /out/driver /driver
ENTRYPOINT ["/driver"]
