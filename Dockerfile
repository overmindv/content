FROM golang:1.26.1-alpine AS build
ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/content ./cmd/content

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && addgroup -S content && adduser -S content -G content
WORKDIR /app
COPY --from=build /out/content /usr/local/bin/content
COPY --from=build /workspace/migrations/schema /app/migrations
USER content
EXPOSE 8080 9090
ENTRYPOINT ["content"]
