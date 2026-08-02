# laserbeak

`laserbeak` выступает как gateway, поэтому типичный поток такой:

1. `laserbeak` держит внешний REST/HTTP API.
2. `bumblebee` держит внутренний gRPC контракт в `api/bumblebee/`.
3. `laserbeak` импортирует этот контракт и вызывает текущий сервис через generated gRPC client.

В шаблоне CRUD намеренно оставлен в proto-контракте как пример интеграции gateway -> service.
