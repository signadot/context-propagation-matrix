# Builds the uninstrumented Go components (sink recorder + test driver).
# Keep the image's Go at or above the `go` directive in go.mod (Dependabot
# bumps raise it; the image has GOTOOLCHAIN=local and cannot download newer).
FROM golang:1.26 AS build
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
