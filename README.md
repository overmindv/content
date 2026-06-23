# bumblebee

`bumblebee` — базовый Go-сервис контента Overmindv. Сейчас он поднят из сервисного шаблона и содержит временный CRUD `TemplateItem`, который позже будет заменён реальными сущностями content-domain.

## На данный момент это заглушкка!

сервис сделан на основе шаблона

я еще не сделал реальные сущности и интеграцию с другими сервисами, буду доделывать

архитектура будет меняться.

## Что уже есть

- `cmd/bumblebee/main.go` как единая точка входа.
- `api/bumblebee/bumblebee.proto` с базовым CRUD-контрактом.
- `internal/pkg/api/bumblebee/` для generated `pb.go` файлов.
- `internal/app/bumblebee/` для transport-layer и runtime.
- `internal/pkg/service/` для бизнес-логики.
- `internal/pkg/store/postgres/` для работы с PostgreSQL.
- `internal/pkg/kafka/` для публикации событий.
- `migrations/schema` и `migrations/seeds` через goose.
- `tests/component/` для компонентных тестов с реальной БД.
- `.github/workflows/ci.yaml`, `.golangci.yml`, `Dockerfile`, `docker-compose.yaml`, `Makefile`.

## Структура

```text
.
├── api/bumblebee/              # proto-контракт текущего сервиса
├── cmd/bumblebee/              # main
├── docs/                              # документация и примеры вызовов
├── internal/
│   ├── app/bumblebee/          # gRPC handlers, HTTP probes, runtime
│   ├── app/bumblebee/mapper/   # proto -> dto/domain
│   ├── config/                        # конфиг по env
│   ├── dto/                           # transport DTO
│   ├── mock/                          # generated mocks
│   ├── pb/pg/<other-service>/         # proto / generated clients других сервисов
│   └── pkg/
│       ├── api/bumblebee/          # generated protobuf
│       ├── domain/                        # доменные структуры
│       ├── kafka/                         # publisher и event envelope
│       ├── logger/                        # инициализация логгера
│       ├── mapper/                        # место для shared мапперов
│       ├── metrics/                       # Prometheus metrics
│       ├── service/                       # use cases и adapters to external services
│       ├── singleton/                     # process-wide dependencies
│       ├── statemachine/                  # опциональные state transitions
│       ├── store/postgres/                # postgres store
│       └── validator/                     # input validation
├── migrations/
│   ├── schema/                        # schema migrations через goose
│   └── seeds/                         # seed migrations через goose
└── tests/
    ├── builders/                      # test builders
    ├── component/                     # component tests
    └── scripts/                       # test helper scripts
```

## Быстрый старт

```bash
cp .env.example .env
docker compose up --build
```

По умолчанию сервис поднимает:

- HTTP: `:8080`
- gRPC: `:9090`
- PostgreSQL 18: `localhost:5432`
- Kafka: `localhost:9092`

Проверка:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Миграции нужны для CRUD-методов `TemplateItem`, но не для health/readiness.

## Команды

```bash
make help
make test
make ctest
make generate
make mocks
make db-status
make db-up
make db-down
make db-create MIGRATION_NAME=add_orders
make db-seed
make env-up
make env-down
make run
```

## HTTP endpoints

- `GET /`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`

Подробные примеры лежат в [docs/endpoints.md](github.com/overmindv/bumblebee/docs/endpoints.md).

