# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/diskcli ./cmd/diskcli

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/diskcli /usr/bin/diskcli

ENTRYPOINT ["diskcli"]
