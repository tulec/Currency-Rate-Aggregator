# Currency Rate Aggregator

Сервис на Go для параллельного получения валютных курсов, выбора лучших предложений покупки и продажи, кеширования результатов и хранения истории в PostgreSQL.

Поддерживаемые источники:

- `cbr` — официальный справочный курс Банка России;
- `tbank` — курсы Т-Банка из публичного JSON endpoint;
- `frankfurter` — справочные курсы Frankfurter;
- `mock` — детерминированные локальные данные для разработки и тестов.

## Быстрый старт

### Локальный запуск без PostgreSQL

```powershell
$env:RATE_SOURCES = 'mock'
go run ./cmd/server
```

Проверка:

```powershell
curl.exe http://localhost:8080/health
curl.exe "http://localhost:8080/rates?currency=USD"
```

Без `DATABASE_URL` сервис работает штатно, но endpoints истории недоступны.

### Запуск с реальными источниками

```powershell
$env:RATE_SOURCES = 'cbr,tbank,frankfurter'
go run ./cmd/server
```

Для этого режима требуется доступ в интернет. Отказ одного источника не блокирует результат, если хотя бы один другой источник вернул корректный курс.

### Запуск всего окружения через Docker Compose

```powershell
docker compose up -d postgres app
docker compose ps
```

API:

```text
http://localhost:8080
```

PostgreSQL:

```text
Host: localhost
Port: 5433
Database: currency_rates
User: currency
Password: currency
```

Остановка:

```powershell
docker compose down
```

Удаление контейнеров вместе с данными PostgreSQL:

```powershell
docker compose down -v
```

## Архитектура

```text
cmd/server              composition root и жизненный цикл приложения
internal/bankclient     адаптеры внешних источников
internal/cache          потокобезопасный TTL-кеш
internal/config         конфигурация из переменных окружения
internal/domain         доменные модели, типы и ошибки
internal/httpapi        маршруты, handlers и middleware
internal/metrics        метрики в формате Prometheus
internal/ratelimit      ограничение частоты запросов по IP
internal/service        агрегация и фоновое обновление
internal/storage        PostgreSQL, запросы и миграции
api.http                запросы для HTTP Client IntelliJ IDEA
openapi.yaml            контракт OpenAPI
```

Основная граница бизнес-логики — `internal/service.Aggregator`.

Поток обработки запроса:

1. HTTP handler валидирует параметры.
2. `Aggregator.FetchRates` проверяет кеш.
3. Источники опрашиваются параллельно через `errgroup`, горутины и канал результатов.
4. Некорректные или недоступные источники исключаются из агрегации.
5. Выбираются максимальный курс покупки и минимальный курс продажи.
6. Свежие данные сохраняются в PostgreSQL и TTL-кеш.

`RefreshRates` намеренно обходит чтение из кеша. Планировщик использует этот метод, поэтому фоновое обновление всегда обращается к источникам.

## Источники курсов

### Банк России

`CBRClient` читает официальный XML:

```text
https://www.cbr.ru/scripts/XML_daily.asp
```

Курс ЦБ является справочным, поэтому значения `buy` и `sell` совпадают.

### Т-Банк

`TBankClient` использует публичный JSON endpoint:

```text
https://www.tinkoff.ru/api/v1/currency_rates/
```

Ответ содержит несколько категорий операций. По умолчанию используется:

```text
DebitCardsTransfers
```

Категорию можно изменить:

```powershell
$env:TBANK_RATE_CATEGORY = 'C2CTransfers'
```

Этот endpoint не является стабильным документированным публичным контрактом T-API. Изменение формата или блокировка со стороны банка считаются ожидаемым эксплуатационным риском и обрабатываются как отказ отдельного источника.

### Frankfurter

Frankfurter предоставляет справочные курсы через REST API. Это дополнительный источник данных, а не коммерческий банк.

### Отложенные интеграции

Альфа-Банк, Сбербанк и ВТБ не подключены. Во время исследования не был найден подходящий открытый и стабильный API курсов без клиентской авторизации, а публичные страницы блокировали автоматические запросы.

HTML scraping не добавляется без отдельного решения, fixture-тестов и зафиксированного контракта парсинга.

## Конфигурация

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `PORT` | `8080` | Порт HTTP-сервера |
| `READ_TIMEOUT` | `5s` | Тайм-аут чтения запроса |
| `WRITE_TIMEOUT` | `10s` | Тайм-аут записи ответа |
| `SHUTDOWN_TIMEOUT` | `10s` | Тайм-аут graceful shutdown |
| `CACHE_TTL` | `30s` | Время жизни агрегированного результата |
| `DATABASE_URL` | пусто | DSN PostgreSQL |
| `SCHEDULER_INTERVAL` | `1m` | Интервал фонового обновления |
| `SCHEDULER_CURRENCIES` | `USD,EUR` | Валюты фонового обновления |
| `RATE_LIMIT_REQUESTS_PER_MINUTE` | `60` | Лимит запросов на один IP |
| `RATE_SOURCES` | `cbr` | Список источников через запятую |
| `RATE_SOURCE` | пусто | Устаревшая настройка одного источника |
| `CBR_DAILY_URL` | URL Банка России | XML endpoint ЦБ |
| `FRANKFURTER_BASE_URL` | `https://api.frankfurter.dev/v2` | Базовый URL Frankfurter |
| `TBANK_RATES_URL` | URL Т-Банка | JSON endpoint Т-Банка |
| `TBANK_RATE_CATEGORY` | `DebitCardsTransfers` | Категория курса Т-Банка |

Пример полного локального запуска:

```powershell
docker compose up -d postgres

$env:DATABASE_URL = 'postgres://currency:currency@localhost:5433/currency_rates?sslmode=disable'
$env:RATE_SOURCES = 'cbr,tbank,frankfurter'
$env:SCHEDULER_CURRENCIES = 'USD,EUR'
go run ./cmd/server
```

## HTTP API

Успешные JSON-ответы:

```json
{
  "data": {}
}
```

Ошибки:

```json
{
  "error": "описание ошибки"
}
```

`/metrics` и pprof не используют JSON envelope.

### Health check

```http
GET /health
```

```json
{
  "data": {
    "status": "ok"
  }
}
```

### Агрегированный курс

```http
GET /rates?currency=USD
```

Ответ содержит:

- `best_buy` — максимальный курс покупки;
- `best_sell` — минимальный курс продажи;
- `sources` — все корректные ответы источников;
- `updated_at` — время агрегации.

### Конвертация

```http
GET /convert?from=USD&to=EUR&amount=100
```

RUB используется как базовая валюта:

- иностранная валюта → RUB: `best_buy`;
- RUB → иностранная валюта: `best_sell`;
- иностранная валюта → иностранная валюта: конвертация через RUB.

### История

```http
GET /rates/history?currency=USD&limit=10
```

```http
GET /rates/history/by-date?currency=USD&from=2026-06-01&to=2026-06-06&limit=10
```

`from` и `to` принимают `YYYY-MM-DD` или RFC3339. Значение `to` в формате даты включает весь указанный день.

### Диагностика

```http
GET /metrics
GET /debug/pprof/
```

Полный контракт находится в [openapi.yaml](openapi.yaml).

## HTTP Client

Готовые запросы находятся в [api.http](api.http). Файл поддерживается встроенным HTTP Client IntelliJ IDEA и совместимыми инструментами.

## База данных

Миграции встроены через `embed` и применяются при запуске приложения:

```text
internal/storage/migrations
```

Основная таблица:

```text
rates
```

SQL выполняется с параметрами. Соединения обслуживаются стандартным пулом `database/sql`.

## Разработка и проверки

Проект использует локальные каталоги Go cache, чтобы проверки не зависели от глобального окружения.

### Генерация моков

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache-generate'
go generate ./internal/httpapi
```

### Тесты

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache'
go test ./...
```

### Покрытие

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache-check'
go test ./... -cover
```

### Статический анализ

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache-vet'
go vet ./...
```

### Race detector

```powershell
go test -race ./...
```

## Гарантии и ограничения

- Автоматические тесты не обращаются к интернету.
- Каждый внешний источник тестируется через `httptest.Server` или локальные данные.
- Частичный отказ источников не приводит к отказу всего запроса.
- История недоступна без PostgreSQL, но остальные endpoints продолжают работать.
- Кеш хранится в памяти одного процесса и не распределяется между экземплярами.
- Rate limiter также хранится в памяти процесса.
- Публичный endpoint Т-Банка может измениться без предварительного уведомления.

## Состояние проекта

Выполнены:

- `go test ./...`;
- `go test ./... -cover`;
- `go vet ./...`;
- live smoke-тест реальных источников;
- smoke-тест PostgreSQL, миграций и history API.


