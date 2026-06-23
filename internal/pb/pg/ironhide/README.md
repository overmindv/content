# ironhide

Пример для сервиса-донорa protobuf-контрактов:

1. Скопировать `.proto` или подключить git submodule/subtree с контрактом.
2. Сгенерировать клиента в `internal/pb/pg/ironhide/`.
3. Создать адаптер в `internal/pkg/service/external/ironhide/`, чтобы бизнес-логика не зависела от generated API напрямую.
