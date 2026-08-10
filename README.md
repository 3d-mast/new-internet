# Canopy 5 — стабильнее выбирает путь

**Canopy** — пятая природная версия проекта `new-internet`. Полог леса устойчив за счёт множества перекрывающихся ветвей; Canopy применяет тот же принцип к доверенным сетевым маршрутам: рабочий путь не переключается из-за одного шумного измерения, но при реальном отказе резерв включается сразу.

> Статус: технический preview. TCP, SOCKS5, HTTP CONNECT, TLS 1.3, защищённый relay/backhaul, автопилот, Windows GUI и серверный режим работают. Wintun/TUN, UDP dataplane и QUIC hole punching пока не реализованы.

## Главное в Canopy 5

- подтверждение нового более выгодного маршрута двумя последовательными циклами, пока текущий выход здоров;
- немедленный failover, если текущий выход offline;
- `SelectedExit` фиксируется до запуска локальных proxy listeners — устранено короткое окно со старым/пустым маршрутом;
- bounded retry backoff 1/2/4/8/16 секунд при локальных ошибках настройки;
- управляющие `LICHEN/2` frames ограничены 64 KiB вместо 1 MiB;
- frame с приписанным вторым JSON/payload отклоняется;
- неизвестные JSON-поля сохраняют совместимость с будущими/старыми узлами;
- Linux CI дополнен `go test -race ./...`;
- новый Windows-клиент `canopy.exe`, диагностика `canopy-console.exe` и `canopy.log`.

## Windows

1. Распакуйте релизный ZIP.
2. Запустите `canopy.exe`.
3. Интерфейс откроется по адресу `http://127.0.0.1:47832/`.
4. Если браузер не открылся — `open-canopy.cmd`.
5. Для диагностики — `canopy-console.exe`.

Новый профиль использует `%APPDATA%\Canopy`. Если уже существует `Reef`, `Rhizome`, `Lichen` или `MyceliumOne`, Canopy использует прежний каталог и сохраняет Ed25519-идентичность, доверие и конфигурацию.

## Zero-touch

После обмена подписанным приглашением автопилот сам:

- проверяет доступные endpoints;
- выбирает direct перед relay/backhaul;
- измеряет доступность и задержку;
- удерживает стабильный здоровый маршрут;
- переключается на резерв при отказе;
- безопасно отключает системный прокси Windows, если рабочих выходов больше нет.

## Сервер

Прямой запуск на Linux/VPS:

```bash
./canopy --server --no-browser
```

Docker:

```bash
docker build -t canopy .
docker run -d --name canopy -p 47831:47831/tcp -p 47830:47830/udp -v canopy-data:/data canopy
```

Для узла за сложным NAT всё ещё нужен реально доступный публичный relay/rendezvous. Canopy не заявляет обход firewall, который блокирует используемый транспорт.

## Адресация

Логические IPv4 (`100.64.0.0/10`) и IPv6 ULA (`fd00::/8`) детерминированы Ed25519-ключом. Это overlay-идентификаторы. Пока Wintun/TUN не реализован, они не являются адресом отдельной сетевой карты Windows и не появляются в `ipconfig`.

## Совместимость

- wire protocol: `LICHEN/2`;
- приглашения: `lch1.`;
- config schema остаётся совместимой;
- Reef 4, Rhizome 3 и Lichen остаются в истории GitHub отдельными ветками/PR/release.

## Проверки

GitHub Actions для Canopy обязан пройти `go test`, Linux race detector, `go vet`, JavaScript syntax check, Linux GUI/API smoke-test, Windows GUI/console smoke-test и Docker build. Prerelease создаётся только после зелёных Linux и Windows jobs.

Подробности: `docs/releases/canopy-5.0.0.md`.

## Авторство и лицензия

Первоначальный создатель, автор концепции и прародитель проекта: **Алексей Прокопчук / Alexey Prokopchuk**.

Apache License 2.0. При распространении необходимо сохранять `LICENSE`, `NOTICE`, авторские уведомления и заметки об изменениях.
