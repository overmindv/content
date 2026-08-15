# Content model

`content` хранит самостоятельные контентные документы и их версии.

Сервис не владеет каталогом, пользователями, ролями, заданиями, обсуждениями и review workflow. Другие сервисы могут ссылаться на контент через `content_item.id`, но `content` не хранит связи с вузами, программами, курсами или темами.

## Основные сущности

### ContentItem

Основная карточка контентного документа.

Типы:

- `article`
- `note`
- `summary`
- `theory`

Статусы:

- `draft`
- `published`
- `archived`

Поля:

```text
id
type
status
title
slug
description
current_revision_id
published_revision_id
created_by
updated_by
created_at
updated_at
published_at
archived_at
```

`created_by` и `updated_by` являются opaque user id. `content` не знает модель пользователя и не проверяет роли.

### ContentRevision

Версия тела документа.

Форматы:

- `markdown`
- `typst`

Поля:

```text
id
content_item_id
revision
format
source
source_hash
message
created_by
created_at
```

Каждый `ContentItem` может иметь много ревизий. Пара `content_item_id + revision` должна быть уникальной. Новая ревизия становится текущей через `current_revision_id`.

`source` хранит исходный Markdown/Typst. `source_hash` нужен для проверки изменения тела документа и идемпотентности будущих операций.

### Tag

Тег, принадлежащий контентному домену.

Поля:

```text
id
name
slug
created_at
```

Теги нужны для базовой группировки и поиска по контенту внутри `content`. Если позже появится глобальный сервис таксономии, `tags` можно будет заменить или синхронизировать с ним.

### ContentTag

Связь многие-ко-многим между документами и тегами.

Поля:

```text
content_item_id
tag_id
created_at
```

### ContentAsset

Ссылка на внешний файл или медиа-объект.

Типы:

- `image`
- `attachment`
- `pdf`
- `archive`

Поля:

```text
id
content_item_id
revision_id
asset_id
kind
title
position
created_at
```

`asset_id` является внешним id файла из media/file сервиса. `content` не хранит бинарные файлы.

`revision_id` опционален: asset может относиться ко всему документу или к конкретной ревизии.

## Инварианты

- У документа всегда есть `id`, `type`, `status`, `title`, `slug`.
- `slug` уникален внутри `content`.
- У документа может быть много ревизий, но только одна текущая ревизия.
- Опубликованный документ может иметь отдельную `published_revision_id`, чтобы черновая текущая ревизия не ломала уже опубликованную версию.
- Удаление документа удаляет его ревизии, связи с тегами и asset references.
- Файлы, пользователи, курсы, темы, задания и обсуждения принадлежат другим сервисам.

## Не входит в модель MVP

- metadata-таблица для языка, сложности, reading time и SEO
- связи с вузами, программами, курсами и темами
- роли и права пользователей
- задания, тесты, решения и подсказки
- комментарии, обсуждения и споры
- pull request / review workflow
