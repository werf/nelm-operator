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

FROM gcr.io/distroless/static:nonroot

ENV HOME=/tmp

WORKDIR /

COPY --from=builder /nelm-operator/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
