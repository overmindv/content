FROM golang:1.26.1-alpine AS build

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/content ./cmd/content

FROM alpine:3.22

WORKDIR /app
COPY --from=build /out/content /usr/local/bin/content

EXPOSE 8080 9090

ENTRYPOINT ["content"]
