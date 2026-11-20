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

Для всех операций нужны метаданные `authorization: Bearer <token>` и
`x-user-id: <uuid>`. Используй случайный `CONTENT_GRPC_SERVICE_TOKEN` длиной от 32
байт. Без него операции отклоняются, HTTP-пробы не меняются. Gateway должен брать UUID из
проверенной сессии, а не пересылать заголовок браузера. Поля `created_by` и
`updated_by` в запросах сохранены для совместимости, но игнорируются.

Это внутренний API. Не открывай gRPC в публичную сеть; передавай токен за
пределами локальной среды только по защищенному каналу.

- Автор видит свой материал и его историю, может изменить или удалить их.
- Остальные пользователи видят только опубликованные материалы. `GetContentItem`
  и `ListContentItems` возвращают им опубликованную ревизию и ее вложения, даже
  если автор уже сохранил новую правку.
- Недоступный по ID материал возвращает `NotFound`, как и отсутствующий.
  Отдельных прав для администратора и назначенного рецензента пока нет.

- `CreateContentItem`
- `GetContentItem`
- `ListContentItems`
- `UpdateContentItem`
- `DeleteContentItem`
- `CreateContentRevision`
- `ListContentRevisions`

Примеры:

```bash
grpcurl -plaintext -H "authorization: Bearer ${CONTENT_GRPC_SERVICE_TOKEN}" \
  -H 'x-user-id: 11111111-1111-4111-8111-111111111111' \
  -d '{"type":"article","status":"draft","title":"Starter article","slug":"starter-article","format":"markdown","source":"# Starter article","tags":["go","content"]}' \
  localhost:9090 overmindv.content.ContentService/CreateContentItem

grpcurl -plaintext localhost:9090 list
```
