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

Сервис: `overmindv.bumblebee.BumblebeeService`

- `CreateTemplateItem`
- `GetTemplateItem`
- `ListTemplateItems`
- `UpdateTemplateItem`
- `DeleteTemplateItem`

Примеры:

```bash
grpcurl -plaintext -d '{"name":"starter","description":"created from grpcurl","status":"draft"}' \
  localhost:9090 overmindv.bumblebee.BumblebeeService/CreateTemplateItem

grpcurl -plaintext localhost:9090 list
```
