FROM golang:1.26.1-alpine AS build
ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}
ARG GO_BUILD_PARALLEL=2
ENV GOFLAGS=-p=${GO_BUILD_PARALLEL}
WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,id=overmindv-gomod,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,id=overmindv-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=overmindv-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/content ./cmd/content

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && addgroup -S content && adduser -S content -G content
WORKDIR /app
COPY --from=build /out/content /usr/local/bin/content
COPY --from=build /workspace/migrations/schema /app/migrations
USER content
EXPOSE 8080 9090
ENTRYPOINT ["content"]
