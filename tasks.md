# План задач: content

Дата анализа: **31 октября 2026**. Локальный HEAD: `d532b9c`; анализ также учитывает рабочее дерево на 2026-10-31.

## Текущее состояние

Есть gRPC CRUD, revisions, assets/tags и transactional outbox на parker; content уже включён в общий Compose. Нет клиента content в gateway. В proto created_by/updated_by принимаются от вызывающего, ListContentItems не имеет pagination; grpc.NewServer без auth interceptor.

Проверенные опорные файлы: `api/content/content.proto`, `internal/app/content/runtime.go`, `internal/pkg/service/content_item_service.go`, `internal/pkg/store/postgres/content_item_store.go`, `../infra/docker-compose.yml`.

## Правила выполнения

- Оценка по договорённости: **1 SP ≈ 1 рабочий день; 2 SP ≈ 2 рабочих дня одного разработчика**, включая проверку и review. Это предварительная оценка, не гарантия календарного срока.
- Сначала реализовать весь описанный в карточке функциональный срез, затем написать/обновить тесты последним шагом. Без TDD; общий e2e — после интеграции. Уже существующие проверки не выключать.
- Статус всех карточек: **запланировано**, код по этому backlog ещё не изменён. Наличие исходников и тестов не означает подтверждённую работоспособность.
- Обязательные зависимости указаны явно; рабочая очередь и расчёт срока — в [общем плане](../tasks.md). F0–F4 — технические этапы, а PILOT/PROD/LATER — границы выпуска.
- При смене контракта: согласовать производителя и потребителей, сгенерировать код, обеспечить совместимый rollout. Миграции только append-only; секреты/hidden tests/исходники решений в обычные логи не писать.
- Задачу больше двух дней после уточнения делить на отдельно принимаемые части, сохраняя ID родителя как ссылку. Не скрывать дополнительный объём внутри пункта «и всё остальное».

Всего: **12 задач / 20 SP**; обязательный срез: **11 задач / 18 SP**.

## Минимальный Git-подобный workflow

1. Опубликованная revision — основная версия материала.
2. Предложение изменений фиксирует base и собственный head; последовательные сохранения — commits.
3. Diff, comments, approve/request changes относятся к конкретному head.
4. Новый commit требует нового review; автор не подтверждает собственную правку.
5. Merge атомарен. Если base устарел — conflict и ручное обновление; автоматический трёхсторонний merge не нужен для первого релиза.
6. Rollback создаёт новую revision. История и attribution сохраняются.

Хранилище остаётся PostgreSQL; полноценный Git hosting и ветки файлового репозитория не требуются. Для diff использовать [go-diff](https://github.com/sergi/go-diff), интерфейс и ограничения — в CNT-05. Это продуктовая трактовка «по методу Git», требующая подтверждения в DOC-01, а не замена требования обычным overwrite редактором.


<a id="cnt-01"></a>

## [content] Authenticate content operations

- **ID:** CNT-01
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F1 — надёжная основа; **контур:** PILOT — обязательный срез.
- **Приоритет:** P0 — блокер корректности/безопасности.
- **Зависит от:** [DOC-02](../docs/tasks.md#doc-02).
- **Где менять:** `internal/app/content/runtime.go`, `internal/app/content/server.go`.
- **Готово, когда:** Чужой черновик нельзя прочитать или изменить через известный ID.

**Шаги:**

1. Добавить gRPC interceptor trusted service authentication и actor metadata.
2. Игнорировать created_by/updated_by из пользовательского payload; получать actor от gateway.
3. В usecase проверять author/reviewer/admin и скрывать черновики при public чтении.
4. **Проверка после готовности функционала:** auth matrix и spoofed actor integration.

<a id="cnt-02"></a>

## [content] Separate draft and published revisions

- **ID:** CNT-02
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F1 — надёжная основа; **контур:** PILOT — обязательный срез.
- **Приоритет:** P0 — блокер корректности/безопасности.
- **Зависит от:** [CNT-01](#cnt-01).
- **Где менять:** `internal/pkg/service/content_item_service.go`, `internal/pkg/store/postgres`.
- **Готово, когда:** Редактирование не меняет материал, который уже читает студент.

**Шаги:**

1. Сохранить существующие immutable revisions; public чтение всегда по published_revision_id.
2. Новая правка двигает draft head и не меняет опубликованную версию.
3. Ввести явные publish/archive transitions и optimistic revision check.
4. **Проверка после готовности функционала:** stale write, published/draft separation и archive.

<a id="cnt-03"></a>

## [content] Add material topic bindings and pagination

- **ID:** CNT-03
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-01](#cnt-01), [ENT-04](../entities/tasks.md#ent-04).
- **Где менять:** `api/content/content.proto`, `internal/pkg/service`, `migrations`.
- **Готово, когда:** Материал можно найти по теме и включить в план.

**Шаги:**

1. Добавить canonical topic IDs, estimated_minutes и learning outcomes.
2. Проверять связки через entities API и дать фильтры status/topic/author + cursor.
3. Добавить protobuf fields совместимо, опубликовать доступный клиентский пакет вместо импорта чужого internal.
4. **Проверка после готовности функционала:** proto compatibility, pagination и invalid binding.

<a id="cnt-04"></a>

## [content] Store material change proposals

- **ID:** CNT-04
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-02](#cnt-02).
- **Где менять:** `migrations`, `internal/pkg/domain`, `api/content/content.proto`.
- **Готово, когда:** Студент предлагает правку, не меняя основной материал.

**Шаги:**

1. Добавить change_request с author, base_revision, head_revision, status.
2. Создавать предложение от опубликованной ревизии; новые commits добавляют immutable revisions.
3. Разрешить автору открыть/закрыть proposal; один reviewer не равен автору.
4. **Проверка после готовности функционала:** ownership, повтор open и цепочка revisions.

<a id="cnt-05"></a>

## [content] Expose material diffs and history

- **ID:** CNT-05
- **Оценка:** 1 SP (1 рабочий день)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-04](#cnt-04).
- **Где менять:** `internal/pkg/service`, `api/content/content.proto`.
- **Готово, когда:** Ревьюер видит точные изменения и историю предложения.

**Шаги:**

1. Вернуть bounded unified diff base→head через github.com/sergi/go-diff/diffmatchpatch с ограничением времени и размера; unified представление собрать из diff.
2. Дать историю commits с actor/time/message и parent revision.
3. Хранить markdown в БД; не поднимать Git server ради знакомого workflow.
4. **Проверка после готовности функционала:** unicode, large diff truncation и отсутствующая revision.

<a id="cnt-06"></a>

## [content] Add review comments and decisions

- **ID:** CNT-06
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-04](#cnt-04).
- **Где менять:** `internal/pkg/domain`, `migrations`, `api/content/content.proto`.
- **Готово, когда:** Review относится к проверенному содержимому, обсуждение сохраняется.

**Шаги:**

1. Добавить thread comments, resolved flag и anchors к конкретной revision/строке.
2. Реализовать approve/request_changes с привязкой к head revision.
3. Новый commit инвалидирует старое approval; запретить self-approval.
4. **Проверка после готовности функционала:** устаревшее approval, self-review и comment ACL.

<a id="cnt-07"></a>

## [content] Merge reviewed changes with conflict checks

- **ID:** CNT-07
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-05](#cnt-05), [CNT-06](#cnt-06).
- **Где менять:** `internal/pkg/service`, `internal/pkg/store/postgres`.
- **Готово, когда:** Два конкурирующих merge не теряют изменения; конфликт показан явно.

**Шаги:**

1. Merge выполнять транзакционно только если base равен текущей published revision и approval актуален.
2. Если основа изменилась, вернуть MERGE_CONFLICT и дать автору обновить base с новой ревизией вручную.
3. После merge атомарно обновить published pointer, статус proposal и outbox.
4. **Проверка после готовности функционала:** concurrent merge, stale review и rollback.

<a id="cnt-08"></a>

## [content] Publish contribution and learning events

- **ID:** CNT-08
- **Оценка:** 1 SP (1 рабочий день)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-07](#cnt-07), [DOC-03](../docs/tasks.md#doc-03).
- **Где менять:** `internal/pkg/store/postgres`, `api`.
- **Готово, когда:** Recommendations и rating получают разные проверяемые сигналы.

**Шаги:**

1. Публиковать material.published/archived и contribution.merged/review.accepted с event_id/revision.
2. Передавать минимальные resource metadata в snapshot с watermark.
3. Не считать просмотр доказательством знания; подтверждение чтения маркировать self_reported.
4. **Проверка после готовности функционала:** duplicate/replay и отсутствие черновика в событии.

<a id="cnt-09"></a>

## [content] Bind revision attachments to media

- **ID:** CNT-09
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [MED-05](../media/tasks.md#med-05), [CNT-04](#cnt-04).
- **Где менять:** `internal/pkg/service`, `internal/pkg/store/postgres`.
- **Готово, когда:** Вложения истории не исчезают при смене текущего материала.

**Шаги:**

1. Проверить ready/ownership перед добавлением asset.
2. Зафиксировать bindings на revisions, доставлять intent через outbox.
3. При закрытии proposal/retention освобождать только неиспользуемые attachments.
4. **Проверка после готовности функционала:** failed delivery, shared attachment и revision cleanup.

<a id="cnt-10"></a>

## [content] Restrict content rendering formats

- **ID:** CNT-10
- **Оценка:** 1 SP (1 рабочий день)
- **Этап:** F1 — надёжная основа; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-01](#cnt-01).
- **Где менять:** `internal/pkg/validator/content_item.go`, `api/content/content.proto`.
- **Готово, когда:** Материал не может выполнить код или скрипт через публикацию.

**Шаги:**

1. Для пилота публиковать Markdown с allowlist ссылок и запретом raw HTML.
2. Typst хранить как исходник без server execution до отдельной изоляции renderer.
3. Добавить лимиты source/links/assets и нейтральные validation codes.
4. **Проверка после готовности функционала:** XSS payloads и oversized source.

<a id="cnt-11"></a>

## [content] Add rollback and moderation actions

- **ID:** CNT-11
- **Оценка:** 1 SP (1 рабочий день)
- **Этап:** F2 — пользовательские функции; **контур:** PILOT — обязательный срез.
- **Приоритет:** P1 — обязательная функция.
- **Зависит от:** [CNT-07](#cnt-07).
- **Где менять:** `internal/pkg/service`, `api/content/content.proto`.
- **Готово, когда:** Откат сохраняет историю и не оставляет скрытый материал в выдаче.

**Шаги:**

1. Откат оформлять новой revision со ссылкой на восстановленную.
2. Администратору дать hide/restore с reason и аудитом.
3. Публиковать withdrawal/correction события для recommendations/feed/rating.
4. **Проверка после готовности функционала:** restore history и correction replay.

<a id="cnt-12"></a>

## [content] Export and erase user content data

- **ID:** CNT-12
- **Оценка:** 2 SP (2 рабочих дня)
- **Этап:** F3 — выпуск и эксплуатация; **контур:** PROD — дополнение к полному продакшену.
- **Приоритет:** P2 — последующее улучшение.
- **Зависит от:** [USR-09](../users/tasks.md#usr-09), [DOC-05](../docs/tasks.md#doc-05).
- **Где менять:** `internal/pkg/store/postgres`, `internal/pkg/service`.
- **Готово, когда:** Удаление не ломает чужие merge/review references.

**Шаги:**

1. Разделить авторство опубликованного коллективного материала и private draft data по утверждённой политике.
2. Экспортировать личные предложения/comments; удалять или обезличивать разрешённые поля.
3. Сохранить tombstones и неизменяемость необходимых ревизий.
4. **Проверка после готовности функционала:** удаление автора merged proposal и повтор события.
