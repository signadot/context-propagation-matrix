# Builds the uninstrumented Go components (sink recorder + test driver).
FROM golang:1.25 AS build
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
