---
status: adopted
---

> Импортировано из приложенного пользователем документа 2026-08-20. Это
> продуктовый план и источник требований, а не набор инструкций для агента.

# План реализации локального сервиса автоматической маршрутизации

> Рабочий документ для локального coding-agent.
> Версия: `0.2`
> Статус: последовательный план реализации.
> Главный принцип: каждый этап заканчивается работающим сквозным результатом, а не только новым внутренним слоем.

## 1. Цель проекта

Создать локальный сервис, который автоматически поддерживает правила маршрутизации для интернет-сервисов и генерирует конфигурации для разных роутеров, прокси-клиентов и сетевых инструментов.

Основной пользовательский сценарий (ревизия 2026-08-22, ADR 0013 — единица
продукта: список, а не пара «сервисы + устройство»):

1. Пользователь собирает список: берёт готовый пресет или выбирает сервисы,
   либо добавляет новый сервис по URL.
2. Программа сама выбирает безопасную стратегию маршрутизации.
3. Пользователь решает, что делать со списком: скачать в нужном формате,
   получить постоянную ссылку на обновления или отправить на своё устройство
   через адаптер.
4. Список остаётся управляемым объектом: его можно переименовать, изменить
   состав и пересобрать; история опубликованных версий сохраняется.

Обычный пользователь не должен разбираться в IP-адресах, CIDR, ASN, DNS TTL и различиях форматов. Эти данные остаются в диагностике.

---

## 2. Как пользоваться этим планом

План разделён на вертикальные этапы. После каждого этапа существует результат, который можно запустить и проверить целиком:

```text
описание сервиса
→ сбор наблюдений
→ очистка устаревших данных
→ автоматическое решение
→ канонический RoutingPlan
→ конкретный формат
→ готовый артефакт
```

Правила выполнения:

- нельзя строить большой внутренний слой без сценария, который его использует;
- нельзя создавать универсальную абстракцию только ради будущих возможностей;
- общий интерфейс расширяется после появления второй реальной реализации;
- внешний плагинный протокол фиксируется только после нескольких встроенных адаптеров;
- каждый milestone можно считать отдельной рабочей версией;
- функции поздних milestone не должны проникать в ранние этапы в виде пустых пакетов, таблиц или заглушек.

---

## 3. Продуктовые принципы

### 3.1. Auto-first

Режим `Auto` используется по умолчанию и самостоятельно:

- выбирает подходящий тип правил для целевого устройства;
- предпочитает доменные правила там, где они поддерживаются;
- использует точные IP только когда это действительно нужно;
- исключает устаревшие наблюдения;
- не включает подозрительные широкие сети;
- безопасно уменьшает список под ограничения устройства;
- выбирает renderer;
- проверяет результат;
- публикует новый артефакт без участия пользователя.

Ручная работа с IP и CIDR допускается только в диагностике и расширенном режиме.

### 3.2. Domain-first

Предпочтительный порядок маршрутизации:

1. Доменные правила на стороне клиента.
2. Домены с локальным преобразованием DNS → nftset/ipset или аналогичный набор.
3. Свежие точные IP-адреса.
4. Lossless-агрегация точных адресов.
5. Официальные диапазоны конкретного сервиса.
6. Подтверждённые выделенные сети сервиса.
7. Широкие эвристические диапазоны — только в отдельном расширенном режиме, не в `Auto`.

### 3.3. WHOIS/RDAP не доказывает принадлежность сервиса

Запрещено автоматически превращать один DNS-IP в широкую регистрационную сеть.

WHOIS, RDAP и ASN используются как:

- метаданные;
- информация о владельце сети;
- сигнал возможного конфликта;
- источник кандидатов для анализа;
- диагностические данные.

Они не становятся активным правилом только потому, что один адрес сервиса попал внутрь найденного диапазона.

### 3.4. Наблюдение и решение политики — разные сущности

Факт «DNS вернул этот IP» не означает «этот IP нужно включить в любой профиль».

Нужно разделять:

```text
Observation validity:
  valid
  stale
  archived
  invalid

Policy decision:
  accepted
  rejected
  quarantined
```

Один и тот же свежий IP может быть:

- принят для устройства без доменных правил;
- не нужен для sing-box;
- отклонён из-за конфликта;
- показан только в диагностике.

Поэтому `accepted`, `rejected` и `quarantined` не хранятся как глобальное состояние наблюдения. Это результат построения конкретного `RoutingPlan`.

### 3.5. Старые адреса исчезают автоматически

Для каждого автоматически найденного значения хранятся:

- `first_seen`;
- `last_seen`;
- `valid_until`;
- DNS TTL, если применимо;
- число наблюдений;
- источник;
- версия или отпечаток источника.

Истёкшие значения не попадают в новые планы, но сохраняются в истории.

### 3.6. Наблюдения не смешиваются с каталогом

Нужно раздельно хранить:

- ручное описание сервиса;
- автоматически найденные ресурсы;
- историю наблюдений;
- решения планировщика;
- опубликованные артефакты.

Накопленные IP не записываются обратно в YAML-файлы встроенного каталога.

### 3.7. Renderer не принимает продуктовых решений

Renderer получает готовый `RoutingPlan` и только:

- преобразует его в конкретный формат;
- проверяет синтаксис и ограничения;
- возвращает артефакт или понятную ошибку.

Renderer не определяет:

- принадлежит ли сеть сервису;
- устарел ли IP;
- нужно ли использовать домен вместо IP;
- допустим ли широкий CIDR.

### 3.8. План и артефакт версионируются отдельно

Нужно различать:

```text
PlanSnapshot
  каноническое решение Auto

ArtifactBuild
  результат применения конкретного renderer к PlanSnapshot
```

Один план может быть отрендерен в несколько форматов. Ошибка одного renderer не делает сам план невалидным.

### 3.9. Безопасность реализуется вместе с функцией

Безопасность не является отдельным финальным этапом.

Например:

- HTTP-source сразу получает SSRF-защиту, timeout и лимит размера;
- контейнер сразу запускается непривилегированным пользователем;
- локальный API по умолчанию слушает только loopback;
- browser discovery сразу получает изолированный профиль и ограничения;
- device deployer сразу делает backup и rollback.

---

## 4. Границы релизов

### 4.1. Core MVP

Core MVP заканчивается после milestone 4 и включает:

- локальный запуск;
- встроенный YAML-каталог;
- DNS- и ручные наблюдения;
- SQLite-хранилище истории;
- автоматическое истечение старых IP;
- детерминированную политику `Auto v1`;
- канонический `RoutingPlan`;
- диагностический Raw JSON;
- один реальный renderer для выбранного целевого устройства;
- валидацию результата;
- неизменяемые снимки;
- постоянную ссылку на последний валидный артефакт;
- минимальный интерфейс выбора устройства и сервисов.

Core MVP не обязан автоматически находить все зависимости нового сервиса и не обязан самостоятельно менять конфигурацию роутера.

### 4.2. Discovery Release

Заканчивается после milestone 7 и добавляет:

- новый сервис по URL;
- Public Suffix List;
- DNS- и browser discovery;
- черновик локального `ServiceDefinition`;
- учебную браузерную сессию;
- связи между доменами;
- безопасную автоматическую активацию подтверждённых зависимостей.

### 4.3. Device Automation Release

Заканчивается после milestone 8 и добавляет:

- обнаружение устройства;
- backup;
- deploy;
- verify;
- rollback;
- периодическое обновление.

### 4.4. Extension Release

Заканчивается после milestone 10 и добавляет:

- несколько стабильных встроенных sources, renderers и deployers;
- формальный manifest;
- внешний plugin protocol;
- изоляцию сторонних плагинов.

---

## 5. Что не входит в Core MVP

- анализ произвольных desktop-приложений;
- Android/iOS-агент;
- pcap-анализ;
- Certificate Transparency;
- автоматическая классификация десятков типов компонентов;
- числовые модели `confidence = 0.83` и `risk = 0.42`;
- полноценный оптимизатор с risk budget;
- внешний plugin host;
- магазин плагинов;
- автоматическая установка на роутер;
- PostgreSQL;
- Redis и отдельный брокер очередей;
- микросервисы;
- Prometheus и OpenTelemetry;
- multi-tenant SaaS;
- глобальная дедупликация профилей разных пользователей;
- стабильные каналы, pinning и сложная политика релизов;
- автоматическое присвоение сервису всех сетей ASN или облачного провайдера.

---

## 6. Технологический стек

### Core MVP

```text
Ядро, CLI и локальный HTTP API: Go
IP/CIDR: net/netip
Хранилище: SQLite в WAL-режиме
Каталог: YAML
Артефакты: локальная директория данных
Локальный запуск: один бинарник + Docker Compose
Тесты и CI: go test + GitHub Actions
UI: Nuxt 3 добавляется только в milestone 4
Browser discovery: Playwright добавляется только в milestone 6
```

Используется один исполняемый файл с подкомандами:

```text
routing-agent refresh
routing-agent build
routing-agent serve
routing-agent doctor
```

Отдельные `api`, `worker` и `cli` бинарники не создаются в Core MVP.

### Когда переходить на PostgreSQL

Переход рассматривается только при наличии хотя бы одного подтверждённого требования:

- несколько одновременно работающих workers;
- несколько экземпляров API;
- большой объём browser discovery;
- серверный режим для нескольких пользователей;
- SQLite становится измеренным узким местом.

Поддерживать SQLite и PostgreSQL одновременно в MVP запрещено.

### Когда добавлять Prometheus и OpenTelemetry

Только после разделения процесса на несколько независимо работающих частей или появления реальной потребности в удалённой эксплуатации.

До этого используются:

- структурированные логи;
- таблица `source_runs`;
- простая страница состояния;
- локальная команда `doctor`.

---

## 7. Архитектура Core MVP

```text
┌─────────────────────────┐
│ Service Catalog         │
│ встроенные и локальные  │
│ YAML-определения        │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ Sources                 │
│ DNS / manual            │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ Sighting Store          │
│ SQLite / TTL / history  │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ Planner Auto v1         │
│ чистые правила          │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ RoutingPlan             │
│ независим от формата    │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ PlanSnapshot            │
│ semantic hash           │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ Renderer + Validator    │
└────────────┬────────────┘
             │
┌────────────▼────────────┐
│ ArtifactBuild           │
│ latest / fallback       │
└─────────────────────────┘
```

### Направление зависимостей

```text
domain
  ↑
planner
  ↑
application
  ↑
infrastructure / sources / renderers / api / web
```

Правила:

- `domain` не импортирует SQLite, HTTP, DNS, Playwright, gRPC и UI;
- `planner` состоит преимущественно из чистых функций;
- `application` координирует сценарии;
- инфраструктура реализует интерфейсы, объявленные потребителем;
- renderer зависит от модели `RoutingPlan`;
- planner не знает имён форматов и роутеров;
- deployer не меняет правила плана.

---

## 8. Структура репозитория

Папки создаются по мере появления работающего кода. Не создавать заранее пустые каталоги для поздних milestone.

Начальная структура:

```text
cmd/
  routing-agent/

internal/
  domain/
    service.go
    resource.go
    sighting.go
    rule.go
    plan.go
    target.go

  planner/
    policy.go
    freshness.go
    build_plan.go
    reduce.go
    canonical.go

  application/
    refresh_service.go
    build_profile.go
    publish_artifact.go

  ports/
    catalog.go
    sighting_store.go
    source.go
    renderer.go
    artifact_store.go
    clock.go

  infrastructure/
    catalogyaml/
    sqlite/
    filesystem/
    dnsclient/
    httpapi/

  sources/
    manual/
    dns/

  renderers/
    rawjson/
    keenetic/

catalog/
  builtin/
  local/
  targets/

data/
  routing-agent.db
  snapshots/
  artifacts/

testdata/
  catalog/
  sightings/
  golden/

docs/
  ARCHITECTURE.md
  adr/
```

После появления соответствующих функций могут быть добавлены:

```text
internal/sources/httpfeed/
internal/sources/browser/
internal/renderers/singbox/
internal/deployers/keenetic/
internal/plugins/
web/
```

---

## 9. Каноническая модель

### 9.1. ServiceDefinition

Встроенные определения хранятся в `catalog/builtin`. Автоматически созданные локальные определения — в `catalog/local`.

```yaml
id: example-video
title: Example Video
category: streaming

components:
  web:
    required: true
  auth:
    required: true
  media:
    required: false

seeds:
  - kind: domain_suffix
    value: example.com
    component: web
    source: manual

sources:
  - id: dns-main
    type: dns
    component: web
    config:
      names:
        - example.com
        - api.example.com
```

В файле отсутствуют накопленные IP и CIDR.

### 9.2. Resource

`Resource` — нормализованная сущность, которую можно наблюдать.

```text
ResourceKind:
  domain
  ip
  prefix
  asn
```

Поля:

```text
id
kind
normalized_value
ip_version, если применимо
created_at
```

Для IP и CIDR используются только `netip.Addr` и `netip.Prefix`.

### 9.3. Sighting

`Sighting` — факт, что конкретный источник связал ресурс с сервисом или компонентом.

```text
id
service_id
component_id
resource_id
source_id
source_class
source_revision
first_seen
last_seen
valid_until
ttl_seconds
observation_count
metadata_json
validity
```

`source_class` в первой версии является перечислением, а не числовым весом:

```text
manual
official
observed
community
metadata
```

`validity`:

```text
valid
stale
archived
invalid
```

`validity` вычисляется или материализуется lifecycle-слоем из времени и валидности наблюдения. Source не может самостоятельно пометить запись как принятую политикой.

### 9.4. Relation

В Core MVP необходима только связь CNAME:

```text
source_resource_id
relation_type: cname_to
target_resource_id
service_id
component_id
first_seen
last_seen
valid_until
source_id
```

Обобщённый граф `loaded_by`, `redirects_to`, `depends_on` добавляется в discovery milestone, а не в исходную модель Core MVP.

### 9.5. TargetConstraints

```text
supports_domain_exact
supports_domain_suffix
supports_dynamic_dns_set
supports_ipv4
supports_ipv6
supports_prefixes
max_rules, optional
max_artifact_size, optional
```

В Core MVP возможности целевого устройства задаются встроенным preset. Автоматический probe устройства появляется позже.

### 9.6. TargetProfile

`TargetProfile` описывает устройство или приложение как данные и связывает его с форматом вывода.

```text
id
title
renderer_id
constraints: TargetConstraints
renderer_options
manual_installation_hint
```

Пример:

```yaml
id: example-router
renderer: example-router-script
constraints:
  supports_domain_exact: true
  supports_domain_suffix: true
  supports_dynamic_dns_set: false
  supports_ipv4: true
  supports_ipv6: false
  supports_prefixes: true
  max_rules: 10000
```

Если новый роутер принимает уже существующий формат, для него добавляется только новый `TargetProfile`. Новый renderer нужен лишь при новом формате файла или иной семантике правил.

### 9.7. RouteRule

Только реальные маршрутизационные правила:

```text
RuleKind:
  domain_exact
  domain_suffix
  ipv4
  ipv6
  prefix4
  prefix6
```

Поля:

```text
kind
value
action
service_id
component_id
expires_at, optional
source_class
reason_codes[]
provenance_refs[]
```

ASN, CNAME и HTTP-зависимость не являются `RouteRule`.

### 9.8. RoutingPlan

```text
target_id
profile_key
services[]
rules[]
excluded[]
warnings[]
coverage[]
policy_version
catalog_revision
observation_cutoff
```

`excluded[]` содержит:

```text
candidate
outcome: rejected | quarantined
reason_codes[]
```

Так карантин остаётся решением конкретного плана, а не состоянием наблюдения.

### 9.9. Semantic hash

В semantic hash включаются только стабильные данные:

```text
canonical rules
+ target constraints
+ policy version
+ catalog revision
+ stable fingerprints использованных наблюдений и источников
```

В hash запрещено включать:

- `created_at`;
- случайные ID;
- внутренние ID SQLite;
- URL подписки;
- порядок получения результатов;
- snapshot ID;
- `observation_cutoff` как timestamp: он сохраняется в snapshot, но не меняет semantic hash при неизменном наборе правил.

### 9.10. PlanSnapshot

```text
id
profile_key
routing_plan_hash
routing_plan_json
policy_version
catalog_revision
observation_cutoff
created_at
status
```

### 9.11. ArtifactBuild

```text
id
plan_snapshot_id
renderer_id
renderer_version
artifact_hash
artifact_path
validation_status
validation_message
created_at
status
```

Один `PlanSnapshot` может иметь несколько `ArtifactBuild`.

---

## 10. SQLite-схема Core MVP

Минимальные таблицы:

```text
resources
sightings
relations
source_runs
profiles
plan_snapshots
artifact_builds
subscriptions
```

Не создавать в Core MVP отдельные таблицы:

- `candidate_rules`;
- `confidence_scores`;
- `risk_scores`;
- `policy_assessments`;
- универсальный dependency graph;
- plugins;
- devices.

Решения `accepted`, `rejected`, `quarantined` сохраняются внутри `RoutingPlan` и `PlanSnapshot`.

Запись snapshot и перевод нового артефакта в `latest` должны выполняться транзакционно.

---

## 11. Auto Policy v1

### 11.1. Политика должна быть детерминированной

В первой версии не использовать числовые веса и обучаемые пороги.

Решения принимаются по классам источника, свежести, типу ресурса и возможностям цели.

### 11.2. Reason codes

Минимальный набор:

```text
manual_rule
official_rule
fresh_dns_observation
fresh_community_observation
stale_observation
invalid_resource
unsupported_by_target
not_required_for_domain_capable_target
wide_network_expansion
shared_cdn_or_cloud
critical_direct_conflict
rule_limit_exceeded
optional_component_removed
lossless_collapsed
source_degraded
```

### 11.3. Алгоритм BuildPlan

```text
1. Загрузить ServiceDefinition выбранных сервисов.
2. Получить valid sightings на observation_cutoff.
3. Добавить ручные и официальные доменные правила.
4. Определить, нужны ли IP для TargetConstraints.
5. Если доменные правила достаточны — не добавлять DNS-IP без необходимости.
6. Если IP нужны — добавить только свежие точные адреса.
7. Добавить официальные CIDR, явно описанные источником.
8. Не расширять IP через WHOIS/RDAP/ASN.
9. Проверить critical-direct и известные shared networks.
10. Выполнить безопасное уменьшение списка.
11. Сформировать rules, excluded, warnings и coverage.
12. Канонически отсортировать результат.
13. Рассчитать semantic hash.
```

### 11.4. Безопасное уменьшение списка

В Core MVP поддерживаются только:

1. дедупликация;
2. удаление правил, поглощённых более широким уже принятым правилом;
3. lossless collapse соседних IP и префиксов;
4. исключение необязательных компонентов по заранее заданному порядку;
5. понятная ошибка или частичное покрытие при превышении лимита.

Не реализовывать в Core MVP:

- lossy collapse;
- автоматическое расширение до `/24`, `/16` и других buckets;
- сложный max-rules optimizer;
- числовой risk budget;
- выбор «лучшего» широкого CIDR по эвристической стоимости.

### 11.5. Обязательное поведение для широкой сети

```text
Вход:
  17 точных свежих IP внутри 104.16.0.0/12
  RDAP сообщает сеть 104.16.0.0/12

Auto для IP-only target:
  17 точных IP → accepted
  104.16.0.0/12 → quarantined
  причина → wide_network_expansion + shared_cdn_or_cloud

Auto для domain-capable target:
  доменные правила → accepted
  точные IP → excluded как not_required_for_domain_capable_target
  104.16.0.0/12 → quarantined

Сборка завершается без вопроса пользователю.
```

---

## 12. Жизненный цикл наблюдений

### 12.1. DNS

Начальная политика:

```text
refresh_interval: 30 минут
validity: clamp(4 × DNS TTL, 2 часа, 24 часа)
stale_retention: 90 дней
```

Поведение:

- первый корректный ответ создаёт `valid` sighting;
- повторное наблюдение обновляет `last_seen`, `valid_until` и счётчик;
- после `valid_until` sighting становится `stale`;
- stale не попадает в новые планы;
- при повторном появлении sighting возвращается в `valid`;
- `first_seen` сохраняется;
- после retention запись становится `archived`.

### 12.2. Ручные правила

Ручные правила не истекают без явно заданного срока.

### 12.3. Официальные источники

Добавляются в milestone 5.

Правила:

- прежние данные действуют в пределах grace period;
- ошибка источника не удаляет рабочий артефакт;
- длительная недоступность помечает источник как degraded;
- резкое изменение источника не должно автоматически расширять рабочие правила;
- предыдущий валидный артефакт остаётся доступен.

---

## 13. Расширяемость без преждевременного SDK

### 13.1. Минимальные интерфейсы Core MVP

Интерфейсы объявляются со стороны сценария, который их использует.

```go
type Source interface {
    ID() string
    Observe(ctx context.Context, req ObserveRequest) ([]SightingInput, error)
}
```

```go
type Renderer interface {
    ID() string
    Version() string
    SupportedRuleKinds() []domain.RuleKind
    Render(ctx context.Context, plan domain.RoutingPlan) (domain.Artifact, error)
    Validate(ctx context.Context, artifact domain.Artifact) error
}
```

```go
type ArtifactStore interface {
    Put(ctx context.Context, artifact domain.Artifact) (StoredArtifact, error)
    Open(ctx context.Context, hash string) (io.ReadCloser, error)
}
```

`Deployer` появляется только в milestone 8.

### 13.2. Target profiles

Target profiles загружаются отдельно от renderer и содержат ограничения конкретного устройства или приложения.

Путь первой версии:

```text
catalog/targets/<id>.yaml
```

Application layer:

1. загружает выбранный `TargetProfile`;
2. передаёт его `TargetConstraints` в planner;
3. получает канонический `RoutingPlan`;
4. выбирает renderer по `renderer_id`;
5. до рендера проверяет, что все `RuleKind` поддерживаются форматом.

Так новый роутер, использующий существующий формат, не требует нового Go-пакета.

### 13.3. Встроенные renderer

Порядок появления:

1. Raw JSON — диагностический renderer.
2. Первый реальный целевой renderer.
3. Второй реальный renderer с другим набором поддерживаемых `RuleKind` и иной выходной семантикой.

После третьей реализации можно выделить:

- формальный `RendererDescriptor` с поддерживаемыми типами правил, MIME и расширениями файлов;
- внутренний registry renderer;
- отдельный каталог `TargetProfile`;
- страницу доступных targets.

До этого не создавать универсальный manifest.

### 13.4. Как добавить встроенный renderer

Новый формат должен:

1. находиться в отдельном пакете `internal/renderers/<id>`;
2. реализовывать `Renderer`;
3. перечислять поддерживаемые `RuleKind`;
4. иметь реальную проверку результата;
5. иметь golden fixtures;
6. не изменять входной `RoutingPlan`;
7. быть зарегистрирован в application composition root;
8. иметь короткую инструкцию использования.

Ограничения конкретного устройства не размещаются в renderer: они задаются через `TargetProfile`.

В `planner` запрещены `switch` и `if` по имени renderer или роутера.

### 13.5. Renderer и Deployer — разные расширения

Renderer создаёт конфигурацию.

Deployer:

- подключается к устройству;
- делает backup;
- применяет готовый артефакт;
- проверяет результат;
- откатывает изменения.

Один и тот же renderer может использоваться:

- для ручного скачивания;
- для subscription URL;
- несколькими deployers.

### 13.6. Внешние плагины

Добавляются только после:

- минимум трёх встроенных renderer;
- минимум двух sources;
- минимум одного deployer;
- реальных изменений интерфейсов по итогам их разработки.

Только тогда фиксируются:

- protobuf-контракт;
- versioned gRPC;
- manifest;
- handshake;
- subprocess isolation;
- resource limits;
- permission model.

Стандартный Go `plugin` не использовать.

---

## 14. Безопасность по этапам

### Базовый процесс

С milestone 1:

- API слушает `127.0.0.1` по умолчанию;
- контейнер работает не от root;
- writable доступ разрешён только к `data/`;
- никаких shell-команд для сетевых данных;
- все IP и CIDR строго парсятся;
- размер входных YAML ограничен;
- секреты не выводятся в логи.

### HTTP source

С milestone 5:

- разрешены только `https` и явно разрешённый `http`;
- запрещены `file://` и неизвестные схемы;
- запрещены loopback, private, link-local и metadata endpoints;
- IP проверяется повторно после каждого redirect;
- timeout;
- redirect limit;
- response size limit;
- content validation;
- User-Agent;
- атомарное принятие новой версии источника.

### Browser discovery

С milestone 6:

- отдельный временный профиль;
- запрет доступа к локальной сети по умолчанию;
- ограничение времени сессии;
- ограничение числа запросов и объёма данных;
- очистка профиля после завершения;
- явное отображение домена, который будет открыт.

### Device deployer

С milestone 8:

- credentials хранятся отдельно;
- backup обязателен;
- deploy атомарен, если устройство это позволяет;
- verify обязателен;
- rollback запускается автоматически при ошибке;
- логируются изменения, но не секреты.

### External plugins

С milestone 10:

- checksum;
- protocol version;
- отдельный процесс;
- timeout;
- лимит CPU и памяти;
- отдельный рабочий каталог;
- минимальные разрешения;
- отсутствие доступа к секретам без явного grant.

---

## 15. Последовательный план реализации

### Milestone 0. Технический вертикальный spike

#### Цель

Проверить центральную модель до выбора БД, API и SDK.

#### Реализовать

```text
захардкоженный ServiceDefinition
→ DNS A/AAAA/CNAME
→ in-memory sightings
→ простая freshness policy
→ Auto v1
→ RoutingPlan
→ Raw JSON в stdout
```

#### Ограничения

Не добавлять:

- SQLite;
- HTTP API;
- UI;
- snapshots;
- adapter registry;
- manifest;
- plugins;
- RDAP;
- Playwright.

#### Обязательные тесты

- мусорный IP не проходит;
- IPv4 и IPv6 нормализуются;
- одинаковый ввод даёт одинаковый canonical JSON;
- DNS-IP не расширяется до сети;
- domain-capable target не получает ненужные IP;
- lossless collapse не добавляет новых адресов.

#### Видимый результат

```bash
go run ./cmd/routing-agent spike --service example --target raw
```

Команда печатает детерминированный `RoutingPlan`.

#### Критерий завершения

Главные типы и функция `BuildPlan` доказали жизнеспособность. Если модель приходится ломать уже здесь, её нужно исправить до создания хранилища.

---

### Milestone 1. Минимальное рабочее ядро

#### Цель

Получить постоянно работающий локальный pipeline с историей и очисткой старых IP.

#### Реализовать

- один бинарник `routing-agent`;
- YAML-каталог;
- SQLite WAL;
- migrations;
- `resources`, `sightings`, `relations`, `source_runs`, `profiles`;
- DNS source;
- manual source;
- scheduler внутри процесса;
- lifecycle `valid → stale → archived`;
- `refresh`;
- `build`;
- Raw JSON renderer;
- локальная директория артефактов;
- structured logging;
- `doctor`;
- базовую безопасность процесса.

#### Команды

```bash
routing-agent refresh --service youtube
routing-agent build --target raw-json --service youtube
routing-agent doctor
```

#### Обязательные тесты

- повторное наблюдение обновляет `last_seen`;
- `first_seen` не меняется;
- истёкший IP становится stale;
- stale не попадает в план;
- вернувшийся IP снова используется;
- перезапуск приложения не теряет состояние;
- YAML с невалидным ресурсом отклоняется;
- план одинаков при разном порядке результатов DNS.

#### Видимый результат

Пользователь может обновить сервис и получить актуальный JSON-файл без старых адресов.

#### Не реализовывать

- API;
- Nuxt;
- HTTP feeds;
- browser discovery;
- device deploy;
- отдельный worker;
- универсальный Adapter SDK.

---

### Milestone 2. Первый реальный renderer

#### Цель

Получить файл, который можно вручную применить на реальном целевом устройстве.

#### Выбор первого target

Первым target рекомендуется Keenetic, но перед реализацией нужно выбрать один конкретный документированный механизм и зафиксировать его ограничения в коротком ADR.

Не пытаться в одном milestone поддержать все варианты Keenetic.

#### Реализовать

- один `TargetProfile` с `TargetConstraints`;
- renderer выбранного формата;
- встроенный validator;
- golden fixtures;
- команду сборки;
- инструкцию ручного применения;
- проверку лимита правил;
- понятное сообщение о частичном покрытии.

#### Команда

```bash
routing-agent build \
  --target keenetic \
  --service youtube \
  --service discord \
  --output ./data/artifacts/
```

#### Обязательные тесты

- renderer не меняет `RoutingPlan`;
- одинаковый plan даёт одинаковый файл;
- validator отклоняет сломанный файл;
- неподдерживаемые типы правил выявляются до рендера;
- широкая RDAP-сеть не появляется в файле;
- stale IP отсутствует;
- golden fixture стабилен.

#### Видимый результат

Готовый файл для одного реального устройства.

#### Не реализовывать

- автоматическое подключение к роутеру;
- backup и rollback;
- универсальный device adapter;
- внешний plugin API.

---

### Milestone 3. PlanSnapshot, ArtifactBuild и подписка

#### Цель

Сделать обновления безопасными и повторяемыми.

#### Реализовать

- `plan_snapshots`;
- `artifact_builds`;
- semantic hash;
- immutable snapshot;
- immutable artifact;
- атомарную публикацию;
- указатель `latest`;
- предыдущий валидный fallback;
- SHA-256;
- локальный HTTP API на loopback;
- subscription URL;
- ETag и Last-Modified;
- простую страницу состояния без отдельного frontend-приложения.

#### Минимальный API

```text
GET  /health
GET  /v1/services
GET  /v1/targets
POST /v1/profiles
POST /v1/profiles/{id}/refresh
POST /v1/profiles/{id}/build
GET  /v1/profiles/{id}
GET  /v1/subscriptions/{token}
GET  /v1/snapshots/{id}
GET  /v1/artifacts/{id}
```

#### Не реализовывать

- stable/beta channels;
- pinning устройств;
- сложную дедупликацию профилей;
- отдельное хранение diff;
- UI истории;
- автоматический deploy.

#### Обязательные тесты

- опубликованный snapshot не меняется;
- одинаковый вход даёт одинаковый plan hash;
- новый renderer build не меняет plan snapshot;
- невалидный artifact не становится latest;
- previous valid fallback работает;
- ETag меняется только при изменении артефакта;
- API недоступен извне по умолчанию.

#### Видимый результат

Пользователь получает постоянную локальную ссылку, которую можно использовать для регулярного обновления.

---

### Milestone 4. Минимальный пользовательский интерфейс

#### Цель

Закрыть основной сценарий без CLI и ручной работы с IP.

#### Реализовать

Nuxt 3-приложение только на этом этапе.

Экраны:

1. Выбор устройства или приложения. Формат показывается только в расширенном режиме.
2. Выбор сервисов.
3. Кнопка «Настроить автоматически».
4. Экран результата.
5. Простая диагностика.

По умолчанию показывать:

- статус;
- выбранные сервисы;
- время обновления;
- тип целевого устройства;
- ссылку подписки;
- кнопку скачать;
- результат проверки;
- предупреждение о частичном покрытии, если оно есть.

Скрыть в диагностике:

- IP;
- CIDR;
- ASN;
- TTL;
- source class;
- reason codes;
- excluded и quarantined candidates.

#### Обязательный E2E

```text
пользователь выбирает Keenetic
→ выбирает YouTube и Discord
→ нажимает Auto
→ получает валидный файл и subscription URL
→ после истечения старого IP следующий artifact его не содержит
```

#### Результат

После milestone 4 Core MVP считается готовым.

---

### Milestone 5. Второй source и второй реальный renderer

#### Цель

Проверить, что границы расширения работают не только на первом частном случае.

#### Реализовать

- защищённый HTTP text/JSON source;
- поддержку официальных сетевых фидов;
- grace period и `source_degraded`;
- второй renderer с другим набором поддерживаемых `RuleKind` и иной выходной семантикой, рекомендуется sing-box source JSON;
- реальную валидацию второго формата;
- третий renderer с учётом Raw JSON;

После появления трёх renderer:

- извлечь внутренний registry;
- ввести `RendererDescriptor` для метаданных формата;
- отделить каталог `TargetProfile` от registry renderer;
- убрать ручную регистрацию из обработчиков API;
- документировать шаги добавления нового renderer.

#### Важно

Registry и descriptor извлекаются из работающих реализаций. Не перепроектировать интерфейс целиком, если существующих методов достаточно.

#### Обязательные тесты HTTP source

- SSRF на localhost блокируется;
- private и link-local блокируются;
- redirect в private network блокируется;
- огромный ответ прерывается;
- timeout работает;
- невалидные IP/CIDR не записываются;
- ошибка источника не удаляет предыдущий артефакт.

#### Видимый результат

Один и тот же профиль можно получить минимум в двух практически разных форматах.

---

### Milestone 6. Добавление нового сервиса по URL — базовая версия

#### Цель

Позволить пользователю создать локальный сервис без ручного перечисления IP.

#### Реализовать

```text
URL
→ нормализация через Public Suffix List
→ canonical URL и redirects
→ DNS A/AAAA/CNAME
→ одна управляемая загрузка страницы через Playwright
→ сбор доменов сетевых запросов
→ безопасный ServiceDefinition draft
```

Автоматически принимать в draft:

- исходный registrable domain;
- его поддомены, реально использованные страницей;
- CNAME-цели как relations;
- ручной seed пользователя.

Не активировать автоматически:

- сторонние CDN целиком;
- сторонние auth-платформы как domain suffix;
- рекламные и аналитические домены;
- ASN и RDAP-сети;
- домены, замеченные только в Certificate Transparency.

#### Хранение

Локальный draft сохраняется в `catalog/local/<service-id>.yaml`.

Наблюдаемые IP остаются только в SQLite.

#### Безопасность

- browser context изолирован;
- доступ к локальной сети запрещён;
- сессия ограничена по времени и объёму;
- временный профиль удаляется;
- пользователь видит URL до запуска.

#### Обязательные тесты

- `example.co.uk` нормализуется через PSL, а не ручной список зон;
- same-site домены попадают в draft;
- third-party домен остаётся кандидатом;
- browser request не превращается в широкий CIDR;
- закрытие браузера не оставляет временный профиль;
- новый сервис сразу доступен существующим renderer.

#### Видимый результат

Пользователь вводит один URL и получает безопасный локальный профиль, который уже можно собрать в поддерживаемые форматы.

---

### Milestone 7. Learning session и расширенное discovery

#### Цель

Находить зависимости, которые не появляются при одной загрузке страницы.

#### Реализовать

- управляемую учебную сессию;
- импорт HAR;
- связи `loaded_by`, `redirects_to`, `observed_in_session`;
- базовые компоненты:
  - core;
  - auth;
  - media;
  - voice;
  - downloads;
  - telemetry;
  - advertising;
  - third_party;
- повторяемые сценарии исследования;
- отображение найденных зависимостей в диагностике;
- автоматическую активацию только по детерминированным правилам.

#### Не реализовывать пока

- LLM как источник истины;
- pcap;
- мобильный агент;
- автоматическое владение доменом по одному сертификату;
- графовую БД;
- числовой ML confidence.

#### Политика активации

- core/auth same-site → можно принять;
- media/voice → принять после наблюдения соответствующего действия;
- telemetry/advertising → не считать обязательными;
- shared third-party → хранить как зависимость, не расширять до suffix или ASN;
- неизвестное → оставить кандидатом.

#### Результат

Discovery Release считается готовым.

---

### Milestone 8. Первый Deployer

#### Цель

Автоматически применить уже проверенный артефакт на устройстве.

#### Первый target

Keenetic, если он остаётся основным пользовательским сценарием.

#### Реализовать

```go
type Deployer interface {
    ID() string
    Probe(ctx context.Context, connection Connection) (DeviceInfo, error)
    Backup(ctx context.Context, device DeviceInfo) (BackupRef, error)
    Deploy(ctx context.Context, device DeviceInfo, artifact Artifact) error
    Verify(ctx context.Context, device DeviceInfo, snapshot PlanSnapshot) error
    Rollback(ctx context.Context, device DeviceInfo, backup BackupRef) error
}
```

Задачи:

- probe модели и версии;
- определение совместимого target preset;
- backup;
- deploy;
- verify;
- automatic rollback;
- настройка регулярного обновления, если устройство это поддерживает;
- audit log без секретов.

#### Обязательные тесты

- неподдерживаемая версия отклоняется до deploy;
- без backup deploy не начинается;
- verify failure вызывает rollback;
- повторный deploy идемпотентен;
- renderer и deployer остаются отдельными пакетами;
- секреты не попадают в логи.

#### Результат

Device Automation Release считается готовым.

---

### Milestone 9. Дополнительные встроенные адаптеры

Добавлять по одному, каждый отдельным вертикальным срезом:

1. OpenWrt/dnsmasq nftset.
2. MikroTik.
3. Amnezia.
4. Локальный sing-box deployer.
5. Другие форматы по реальному пользовательскому спросу.

Для каждого:

- `TargetProfile`;
- новый renderer только при новом формате;
- validator;
- golden fixtures;
- инструкция ручного использования;
- deployer только если он даёт реальную пользу;
- E2E на реальном или воспроизводимом тестовом окружении.

Количество форматов не является метрикой качества.

---

### Milestone 10. Внешние плагины

#### Предварительные условия

- интерфейсы встроенных адаптеров стабилизировались;
- есть минимум три renderer;
- есть минимум два sources;
- есть минимум один deployer;
- известны реальные различия и ограничения.

#### Реализовать

- protobuf contracts;
- versioned gRPC;
- plugin discovery;
- handshake;
- protocol negotiation;
- manifest;
- subprocess isolation;
- permissions;
- timeout;
- resource limits;
- checksum;
- структурированные логи;
- пример внешнего renderer;
- пример внешнего source;
- SDK и шаблон репозитория.

#### Критерии

- несовместимый plugin отклоняется до работы;
- падение plugin не завершает основной процесс;
- зависший plugin завершается;
- plugin не видит секреты без permission;
- protocol version документирован;
- встроенные адаптеры продолжают работать без plugin host.

---

### Milestone 11. Масштабирование и наблюдаемость

Выполняется только при измеренной необходимости.

Возможные задачи:

- отделить API от worker;
- перейти с SQLite на PostgreSQL;
- добавить очередь заданий;
- добавить несколько browser workers;
- вынести артефакты в S3-compatible storage;
- Prometheus;
- OpenTelemetry;
- multi-instance locking;
- серверный режим;
- резервное копирование и восстановление PostgreSQL.

Нельзя выполнять этот milestone «на будущее» без измеренной причины.

---

## 16. Тестовая стратегия

### 16.1. Главное правило

Каждый milestone обязан иметь сквозной тест своего пользовательского результата.

Тесты внутренних функций не заменяют E2E.

### 16.2. Unit tests

- строгий парсинг IP/CIDR;
- canonical sorting;
- semantic hash;
- lifecycle;
- правила Auto v1;
- safe reducer;
- target constraints;
- reason codes.

### 16.3. Property tests

- lossless collapse не расширяет множество адресов;
- перестановка входа не меняет план;
- повторная сериализация не меняет semantic hash;
- stale sighting не попадает в план;
- renderer не изменяет входной план.

### 16.4. Golden tests

Для каждого renderer:

```text
testdata/golden/<renderer>/<case>.input.json
testdata/golden/<renderer>/<case>.expected
```

Golden-файл обновляется только осознанно вместе с описанием изменения формата.

### 16.5. Integration tests

Добавляются вместе с соответствующей функцией:

- SQLite — milestone 1;
- DNS — milestone 1;
- artifact publication — milestone 3;
- HTTP source — milestone 5;
- Playwright — milestone 6;
- device deploy — milestone 8;
- plugin subprocess — milestone 10.

### 16.6. Fuzz tests

Только для реально существующих входных границ:

- IP/CIDR parser;
- YAML catalog parser;
- URL и redirect validation;
- HTTP feed parser;
- plugin manifest после milestone 10.

### 16.7. Нет обязательного процента покрытия

Не устанавливать формальный coverage threshold в Core MVP. Важнее:

- покрыть критичные преобразования;
- иметь regression tests для известных ошибок;
- проверять реальный результат renderer;
- поддерживать сквозные сценарии.

---

## 17. Правила для локального coding-agent

> Это исторический снимок ограничений, сохранённый как часть принятого плана,
> а не действующий источник команд для агента. Актуальные обязательные правила
> находятся в `AGENTS.md`; тематические правила и условные workflow — в
> `.agents/rules/` и `.agents/skills/`. При расхождении действуют эти текущие
> источники; список ниже намеренно не синхронизируется с ними.

1. Каждый milestone заканчивается работающим сквозным сценарием.
2. Не создавать пустые пакеты для будущих функций.
3. Не создавать интерфейс без конкретного потребителя.
4. Интерфейс объявляется на стороне потребителя.
5. Универсальная абстракция извлекается после второй реальной реализации.
6. Не помещать decision-логику в source, renderer или deployer.
7. Не хранить ограничения устройства внутри renderer: renderer описывает формат, `TargetProfile` — устройство.
8. Не хранить автоматически найденные IP в YAML-каталоге.
9. Не хранить `accepted` или `quarantined` как глобальный статус sighting.
10. Не использовать числовые confidence/risk в Auto v1.
11. Не расширять DNS-IP через WHOIS, RDAP или ASN.
12. Не использовать shell для сетевых данных.
13. Не публиковать artifact до прохождения validator.
14. Не изменять опубликованные PlanSnapshot и ArtifactBuild.
15. Не включать время и внутренние ID в semantic hash.
16. Не создавать внешний plugin host до milestone 10.
17. Не добавлять PostgreSQL, Redis, Prometheus или OpenTelemetry без измеренной необходимости.
18. Безопасность источника реализуется в том же milestone, что и сам источник.
19. Не начинать новый milestone, пока текущий E2E не проходит.
20. Исправление архитектуры допускается на раннем milestone; не сохранять плохую абстракцию ради уже написанного кода.
21. LLM можно использовать для объяснений и предложения кандидатов, но не как доказательство сетевого правила.
22. Любое принятое правило должно иметь `reason_codes` и provenance.
23. Любое отклонённое правило должно иметь машинно читаемую причину.
24. Все команды обновления и сборки должны быть идемпотентными.
25. ADR создаётся только для реального выбора, который трудно изменить локально.
26. `CHANGELOG.md` начинается с первой публикуемой версии, а не с технического spike.

---

## 18. Минимальные архитектурные документы

До Core MVP достаточно:

```text
docs/ARCHITECTURE.md

docs/adr/001-domain-first-and-no-network-expansion.md
docs/adr/002-routing-plan-renderer-boundary.md
```

Следующие ADR создаются перед соответствующей реализацией, а не заранее:

```text
SQLite → PostgreSQL migration
external plugin protocol
device deployment model
browser discovery isolation
```

Принятый ADR не переписывается задним числом. Новое решение создаёт заменяющий ADR.

---

## 19. Definition of Done Core MVP

Core MVP готов, когда работает сценарий:

```text
1. Пользователь запускает локальное приложение.
2. Выбирает поддерживаемое устройство.
3. Выбирает YouTube и Discord.
4. Система обновляет DNS-наблюдения.
5. Истёкшие IP автоматически исключаются.
6. Широкие WHOIS/RDAP-сети не попадают в Auto.
7. Planner строит детерминированный RoutingPlan.
8. PlanSnapshot сохраняется отдельно от формата.
9. Renderer создаёт валидный файл для устройства.
10. ArtifactBuild публикуется как latest.
11. Пользователь получает файл и subscription URL.
12. Следующее обновление проходит без работы с IP/CIDR.
13. При ошибке нового renderer остаётся доступен предыдущий валидный artifact.
14. В диагностике видна причина включения и исключения каждого правила.
```

Core MVP не считается незавершённым из-за отсутствия:

- автоматического добавления нового сервиса;
- автоматического deploy на роутер;
- внешних plugins;
- PostgreSQL;
- десятков форматов.

---

## 20. Definition of Done Discovery Release

```text
1. Пользователь вводит URL нового веб-сервиса.
2. URL нормализуется через Public Suffix List.
3. DNS и браузер находят связанные ресурсы.
4. Same-site зависимости формируют локальный draft.
5. Сторонние сервисы не активируются широкими правилами.
6. Пользователь может провести learning session.
7. Подтверждённые зависимости автоматически попадают в профиль.
8. Новый сервис сразу доступен всем встроенным renderer.
9. Старые DNS-IP нового сервиса также очищаются по lifecycle.
```

---

## 21. Метрики качества

Главные показатели:

- доля профилей, обновляющихся без ручной работы с IP;
- число ложных маршрутизаций посторонних сервисов;
- доля stale IP, своевременно исчезнувших из артефактов;
- процент успешных renderer validations;
- процент обновлений, в которых previous valid fallback не понадобился;
- время от добавления URL до рабочего локального профиля;
- число форматов, добавленных без изменения planner.

Не считать главными метриками:

- количество интерфейсов;
- количество пакетов;
- количество поддерживаемых форматов само по себе;
- процент покрытия тестами без учёта E2E;
- сложность внутренней архитектуры.

---

## 22. Ближайший порядок работ

```text
Milestone 0
  hardcoded service
  → DNS
  → in-memory sightings
  → Auto v1
  → RoutingPlan
  → Raw JSON

Milestone 1
  YAML
  → SQLite
  → lifecycle
  → CLI
  → persistent Raw JSON artifact

Milestone 2
  first real target
  → renderer
  → validator
  → manually usable file

Milestone 3
  PlanSnapshot
  → ArtifactBuild
  → latest
  → fallback
  → subscription

Milestone 4
  minimal UI
  → Core MVP
```

Только после этого:

```text
HTTP official sources
→ second renderer
→ internal registry
→ URL discovery
→ learning session
→ device deploy
→ more adapters
→ external plugins
→ scaling
```

Такое разделение сохраняет расширяемое ядро, но не требует построить всю платформу до первого полезного результата.

---

## 23. Изменения относительно версии 0.1

- горизонтальные слои заменены вертикальными milestone;
- Core MVP сокращён до реально достижимого продукта;
- browser discovery вынесен в отдельный релиз;
- device deployment вынесен после стабильного renderer;
- PostgreSQL заменён на SQLite для локального MVP;
- отдельные API/worker/CLI процессы заменены одним бинарником;
- Prometheus и OpenTelemetry отложены;
- Observation validity отделена от решения политики;
- числовые confidence/risk убраны из Auto v1;
- `PlanSnapshot` отделён от `ArtifactBuild`;
- общий Adapter SDK заменён минимальными интерфейсами;
- renderer, target profile и deployer разделены;
- новый роутер может переиспользовать существующий renderer через TargetProfile;
- manifest и registry появляются после нескольких реальных адаптеров;
- внешний plugin protocol отложен до стабилизации встроенных расширений;
- сложный optimizer заменён безопасным reducer;
- безопасность перенесена внутрь соответствующих milestone;
- правила coding-agent изменены так, чтобы не провоцировать преждевременные абстракции.
