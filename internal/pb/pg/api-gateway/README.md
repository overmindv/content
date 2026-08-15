# api-gateway

`api-gateway` выступает как gateway, поэтому типичный поток такой:

1. `api-gateway` держит внешний REST/HTTP API.
2. `content` держит внутренний gRPC контракт в `api/content/`.
3. `api-gateway` импортирует этот контракт и вызывает текущий сервис через generated gRPC client.

В шаблоне CRUD намеренно оставлен в proto-контракте как пример интеграции gateway -> service.
