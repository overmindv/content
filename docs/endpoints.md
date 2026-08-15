# Endpoints

## HTTP

- `GET /` возвращает базовую информацию о сервисе.
- `GET /healthz` liveness probe.
- `GET /readyz` readiness probe с проверкой PostgreSQL.
- `GET /metrics` Prometheus metrics.

Пример:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/metrics
```

## gRPC

Сервис: `overmindv.content.ContentService`

- `CreateContentItem`
- `GetContentItem`
- `ListContentItems`
- `UpdateContentItem`
- `DeleteContentItem`
- `CreateContentRevision`
- `ListContentRevisions`

Примеры:

```bash
grpcurl -plaintext -d '{"type":"article","status":"draft","title":"Starter article","slug":"starter-article","format":"markdown","source":"# Starter article","tags":["go","content"]}' \
  localhost:9090 overmindv.content.ContentService/CreateContentItem

grpcurl -plaintext localhost:9090 list
```
