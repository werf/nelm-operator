ARG DENO_VERSION=2.7.1

FROM golang:1.25 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /nelm-operator

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

FROM denoland/deno:bin-${DENO_VERSION} AS deno

# Deno is dynamically linked against glibc, so the final image cannot be distroless/static.
FROM gcr.io/distroless/cc-debian12:nonroot

ENV HOME=/tmp \
    NELM_DENO_BINARY_PATH=/usr/local/bin/deno

WORKDIR /

COPY --from=deno /deno /usr/local/bin/deno
COPY --from=builder /nelm-operator/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
