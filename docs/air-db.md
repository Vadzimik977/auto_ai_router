# air_db — нативный источник credentials и моделей (Postgres)

`air_db` — третий источник данных маршрутизации (рядом с `config.yaml` и
`litellm_db`), чьи таблицы **зеркалят формат jsonnet из services**
(`credentials.libsonnet` / `models.libsonnet`). В отличие от `litellm_db`:

- API-ключи в БД **не хранятся** — только референсы `os.environ/KEY01`
  (значения ключей остаются в Vault/ExternalSecret или `.local` при локальной
  разработке); шифрование значений и signing key не нужны;
- поддерживаются все балансировочные поля: `weight`, `priority`,
  `fallback_priority`, `is_fallback`, `proxy_url`, `scopes`/`denied_scopes`,
  `reasoning_only`;
- `priority: true` делает air_db **авторитетным источником** для креда: запись
  из БД заменяет одноимённую запись из `config.yaml`.

Роль `litellm_db` при включённом air_db не меняется: аутентификация ключей
(`LiteLLM_VerificationToken`), бюджеты и записи расходов остаются за ним.
Отключается только его подгрузка моделей/кредов (`load_db_models` / sync) —
air_db берёт эту роль на себя.

## Конфигурация

```yaml
air_db:
  enabled: true
  is_required: true        # false по умолчанию: при ошибке подключения роутер стартует без air_db
  priority: true           # false по умолчанию: тогда air_db работает аддитивно, как litellm_db
  database_url: "os.environ/AIR_DATABASE_URL"
  max_conns: 10
  min_conns: 2
  connect_timeout: 5s
  sync_interval: 1m        # как litellm_db.db_model_sync_interval
```

## Схема

```sql
CREATE TABLE air_credentials (
  name               text PRIMARY KEY,
  type               text NOT NULL,            -- openai|gemini|anthropic|vertex-ai|proxy|air|vllm|...
  api_key_env        text,                     -- 'os.environ/KEY01' — как в jsonnet
  api_key            text,                     -- только локальные/тестовые ключи (не для прода)
  base_url           text,
  proxy_url          text,                     -- egress: 'http://<region>.egress:3128'
  weight             int  NOT NULL DEFAULT 1,
  priority           int  NOT NULL DEFAULT 0,
  fallback_priority  int,
  is_fallback        boolean NOT NULL DEFAULT false,
  rpm                int  NOT NULL DEFAULT -1,
  tpm                int  NOT NULL DEFAULT -1,
  scopes             text[] NOT NULL DEFAULT '{}',
  denied_scopes      text[] NOT NULL DEFAULT '{}',
  reasoning_only     boolean NOT NULL DEFAULT false,
  project_id         text,                     -- vertex
  location           text,
  credentials_file   text,                     -- путь к JSON сервис-аккаунта (vertex)
  credentials_json   text,
  updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE air_models (
  name                  text NOT NULL,
  credential            text NOT NULL REFERENCES air_credentials(name) ON DELETE CASCADE,
  model                 text,                 -- реальное имя у провайдера; NULL/равно name = алиас
  rpm                   int NOT NULL DEFAULT -1,
  tpm                   int NOT NULL DEFAULT -1,
  weight                int NOT NULL DEFAULT 0,
  passthrough_responses boolean,
  websocket_responses   boolean,
  passthrough_messages  boolean,
  default_params        jsonb,                -- vllm: chat_template_kwargs, temperature, ...
  updated_at            timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (name, credential)
);
```

Строки **удаляются** при исчезновении из источника — таблицы синхронизируются
сидером из jsonnet (см. «Сидер» ниже), поэтому единственный способ убрать кред
из маршрутизации — убрать его из jsonnet.

## Разграничение по инстансам (региональные роутеры)

Выбран вариант A: **у каждого AIR-инстанса своя база/схема**, как сейчас у
каждого свой `config.yaml`. Региональный роутер (`ru01`, `ru02`, ...) читает
только свою базу, сидер для него рендерит именно его `params.libsonnet` —
поэтому «трафик ru01 → ger01, ru02 → ger03» соблюдается автоматически:

- правило egress живёт в jsonnet (`viaEgress('ger01', ...)` проставляет
  `proxy_url: 'http://ger01.egress:3128'` каждому креду региона);
- сидер переносит результат в `air_credentials.proxy_url` без собственной
  логики — разойтись с services она не может;
- роутер применяет `proxy_url` на уровне запроса
  (`httputil/client.go:184`), ничего не зная о «регионах».

Схема при этом не меняется: PRIMARY KEY остаётся `name` (внутри одной базы
коллизий нет — каждый инстанс наполняется только своими аккаунтами).
Если в будущем понадобится один общий Postgres на все регионы — это вариант B:
колонка `router` + PRIMARY KEY `(router, name)` + `WHERE router = $1` в fetch
(перечислено в обсуждении PR, в код не входит).

## Семантика

- При `priority: true` статический (YAML) кред с именем, которое объявляет
  air_db, исключается из маршрутизации на уровне балансировщика
  (`RoundRobin.UpdateDBCredentials(dbCreds, priority)`): запись из БД реально
  занимает это имя (свой api_key/base_url/вес), а не только числится
  переопределённой в служебных списках. Совпадения логируются при старте и
  каждом синке. Покрыто тестом `TestUpdateDBCredentials_Priority`
  (`internal/balancer/roundrobin_test.go`).
- Модели всегда **мержатся** со статическим списком (как у litellm_db:
  `UpdateDBModels` дополняет по имени, статик-снапшот менеджера неизменяем).
  Чтобы модель жила только в БД — не объявляйте её в `config.yaml`.
- При `priority: false` air_db работает аддитивно: добавляются только имена,
  которых нет в статике.
- Синхронизация — раз в `sync_interval` (дефолт 1m), рестарт не нужен.

## Сидер (в services)

Планируемый `scripts/local/seed.py` + `make local-seed`: **один запуск = один
инстанс** (`make local-seed AIR_INSTANCE=ru01`, цель рендерит `ru01/params.libsonnet`
и пишет в базу этого инстанса):

1. рендерит те же `params.libsonnet` (после `fromGroups`/`viaEgress`), что дают
   `config.yaml`;
2. UPSERT по primary key, DELETE отсутствующих имён;
3. api-ключи не записывает — только `os.environ/...` референсы;
4. контрактный тест: набор имён сида == набору имён рендера config.yaml
   этого же инстанса.

## Локальная разработка

`services` compose поднимает `postgres` + `litellm-schema`; после миграции схемы
air_db и сида запускается роутер с секцией `air_db` из `.local` конфига.
`docker-compose.local.yml` в auto_ai_router остаётся для litellm-варианта.