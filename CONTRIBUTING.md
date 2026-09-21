# Contributing / Участие в разработке

Спасибо за интерес к ArtBay Publisher! Thank you for helping improve ArtBay Publisher.

1. Create a focused branch and keep each change scoped to one problem.
2. Never commit `golden_key`, Telegram tokens, account exports, logs, backups, or product archives.
3. Run `go test ./...` and `go vet ./...` before opening a pull request.
4. Describe the user-visible behavior, verification performed, and any compatibility impact.
5. Security reports must follow [`SECURITY.md`](SECURITY.md), not public issues.

Пожалуйста, делайте небольшие тематические изменения, не добавляйте секреты и
пользовательские данные, запускайте тесты и подробно описывайте проверку в PR.
