# Builds the uninstrumented Go components (sink recorder + test driver).
#
# go.mod's `go` directive is the single source of truth for the Go version.
# The image is only a bootstrap: it ships with GOTOOLCHAIN=local, so when a
# Dependabot bump raises the directive past the image, `go mod download`
# refuses. `auto` lets `go` fetch the toolchain go.mod asks for instead.
# scripts/check-toolchains.sh still requires the image tag to be at or above
# the directive, so the download is a safety net, not the steady state.
FROM golang:1.26 AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
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
