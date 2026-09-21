# ArtBay Publisher 4.0.2 — reliable queue cancellation

## Русский

### Главное

- Добавлена команда `/cancel`, которая окончательно отменяет массовую публикацию и удаляет checkpoint.
- Кнопка **«Отмена»** теперь работает не только в предпросмотре, но и после запуска очереди.
- `/stop` однозначно означает паузу с возможностью `/resume`.
- Исправлена блокировка нового ZIP после отмены старой массовой публикации.

### Уже входит в V4

- Безопасная локальная панель подключения Telegram и FunPay.
- Проверка Bot Token и `golden_key` до сохранения.
- Массовые ZIP/JSON-пакеты, checkpoint после каждого лота и восстановление после перезапуска.
- Раздельный учёт дубликатов, переполненных категорий и настоящих ошибок.
- PNG/JPG/WEBP/GIF, кеширование одинаковых изображений, экспорт и rollback.

## English

### Highlights

- Added `/cancel` to permanently stop bulk publishing and delete its checkpoint.
- The **Cancel** preview button now remains effective after the queue starts.
- `/stop` is now explicitly a resumable pause operation.
- Fixed new ZIP uploads being blocked by an already-cancelled queue.

### Included in V4

- Secure local Telegram and FunPay setup dashboard.
- Bot Token and `golden_key` validation before saving.
- Large ZIP/JSON imports, per-offer checkpoints, and restart recovery.
- Separate duplicate, category-capacity, and real error counters.
- PNG/JPG/WEBP/GIF support, image reuse, offer export, and rollback.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- Runtime dashboard verified on `127.0.0.1:8765` as version `4.0.2`.
