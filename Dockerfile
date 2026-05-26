FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY nelm-operator/go.work nelm-operator/go.mod nelm-operator/go.sum nelm-operator/
COPY nelm/go.mod nelm/go.sum nelm/
COPY common-go/go.mod common-go/go.sum common-go/
COPY kubedog/go.mod kubedog/go.sum kubedog/

WORKDIR /workspace/nelm-operator
RUN go mod download
WORKDIR /workspace

COPY nelm-operator/ nelm-operator/
COPY nelm/ nelm/
COPY common-go/ common-go/
COPY kubedog/ kubedog/

WORKDIR /workspace/nelm-operator

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
ENV HOME=/tmp
WORKDIR /
COPY --from=builder /workspace/nelm-operator/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
