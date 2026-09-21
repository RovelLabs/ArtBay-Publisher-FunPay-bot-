# ArtBay Publisher 4.0.4 — installer, icon, and README fixes

## Русский

### Главное

- Добавлен полноценный **установщик для Windows** (`ArtBayPublisher-Setup-4.0.4.exe`, на базе Inno Setup): создаёт папку установки, ярлыки в меню «Пуск» и на рабочем столе, регистрирует программу в «Установка и удаление программ». Права администратора не требуются.
- Добавлена фирменная **иконка приложения**, встроенная в `.exe` вместе с версией и данными о разработчике (`go-winres`).
- Исправлена ошибка рендеринга Mermaid-диаграммы архитектуры в README (GitHub не мог отобразить блок-схему из-за спецсимвола в подписи узла).
- Портативная версия (`ArtBayPublisher-v4.0.4-windows-x64.zip`) осталась без изменений в логике — только обновлённая версия.

### Уже входит в V4

- Безопасная локальная панель подключения Telegram и FunPay.
- Проверка Bot Token и `golden_key` до сохранения.
- Массовые ZIP/JSON-пакеты, checkpoint после каждого лота и восстановление после перезапуска.
- Раздельный учёт дубликатов, переполненных категорий и настоящих ошибок.
- PNG/JPG/WEBP/GIF, кеширование одинаковых изображений, экспорт и rollback.
- Команда `/cancel` для окончательной отмены очереди.

## English

### Highlights

- Added a full **Windows installer** (`ArtBayPublisher-Setup-4.0.4.exe`, built with Inno Setup): creates the install folder, Start Menu / Desktop shortcuts, and registers an uninstaller. No admin rights required.
- Added a branded **application icon**, embedded into the `.exe` together with version and publisher metadata (`go-winres`).
- Fixed a Mermaid architecture-diagram rendering error in the README (GitHub failed to render the flowchart because of a special character in a node label).
- The portable build (`ArtBayPublisher-v4.0.4-windows-x64.zip`) is unchanged apart from the version bump.

### Included in V4

- Secure local Telegram and FunPay setup dashboard.
- Bot Token and `golden_key` validation before saving.
- Large ZIP/JSON imports, per-offer checkpoints, and restart recovery.
- Separate duplicate, category-capacity, and real error counters.
- PNG/JPG/WEBP/GIF support, image reuse, offer export, and rollback.
- `/cancel` for permanently discarding a saved queue.

## Verification

- `go test -count=1 ./...`
- `go vet ./...`
- Runtime dashboard verified on `127.0.0.1:8765` as version `4.0.4`.
- Installer built and run locally: shortcuts, uninstaller entry, and icon confirmed.
