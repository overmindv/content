FROM golang:1.26.1-alpine AS build

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/bumblebee ./cmd/bumblebee

FROM alpine:3.22

WORKDIR /app
COPY --from=build /out/bumblebee /usr/local/bin/bumblebee

EXPOSE 8080 9090

ENTRYPOINT ["bumblebee"]
