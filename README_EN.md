<div align="center">
  <img src="docs/assets/banner.svg" alt="ArtBay Publisher — Telegram to FunPay publishing automation" width="100%">

  <p>
    <a href="README.md">Русский</a> ·
    <a href="README_EN.md"><b>English</b></a>
  </p>

  <p>
    <img alt="Version" src="https://img.shields.io/badge/version-4.0.4-6d82ff?style=for-the-badge">
    <img alt="Go" src="https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white">
    <img alt="Windows" src="https://img.shields.io/badge/Windows-10%20%7C%2011-0078D4?style=for-the-badge&logo=windows11&logoColor=white">
    <img alt="Telegram" src="https://img.shields.io/badge/Telegram-Bot-26A5E4?style=for-the-badge&logo=telegram&logoColor=white">
    <img alt="FunPay" src="https://img.shields.io/badge/FunPay-Publisher-ff5a5f?style=for-the-badge">
  </p>

  <h3>A local-first Telegram ↔ FunPay bridge for reliable bulk offer publishing</h3>
</div>

> [!IMPORTANT]
> ArtBay Publisher is an independent RovelLabs project. It is not an official FunPay or Telegram product. Use automation responsibly and follow the platforms' rules.

## ✨ Highlights

| Feature | What it does |
|---|---|
| 🔐 Secure connection | Validates the Telegram Bot Token and FunPay `golden_key` before saving; stored secrets are never rendered back |
| 📦 Flexible imports | Accepts single-lot ZIPs, large nested ZIP archives, `lots.json`, `artbay-batch.json`, and image-free JSON updates |
| 🚀 Bulk publishing | Durable progress, pause/resume, cancellation, and recovery after an app restart |
| 🧠 Smart failures | Separates duplicates and full categories from genuine per-offer errors |
| ⏯ Queue controls | `/stop` pauses, `/resume` continues, and `/cancel` permanently discards a saved queue |
| 🛍 Offer management | Browse, price, enable/disable, clone, export, delete, and roll back offers |
| 🖼 Image support | PNG, JPG, WEBP, and GIF with SHA-256-based image reuse |
| 🏠 Local-first | The dashboard is bound to `127.0.0.1:8765`; user data stays on the local machine |

## 🧭 Architecture

```mermaid
flowchart LR
    A[ZIP / JSON] --> B[Telegram bot]
    B --> C[ArtBay Publisher<br>localhost:8765]
    C --> D{Package validation}
    D -->|ready| E[Publishing queue]
    D -->|invalid| F[Clear error message]
    E --> G[FunPay]
    E --> H[Disk checkpoint]
    H -->|resume| E
```

## 🚀 Quick start

Open the [latest release](https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-/releases/latest) and pick one:

| Option | File | Use it when |
| :--- | :--- | :--- |
| 🛠 **Installer (recommended)** | `ArtBayPublisher-Setup-4.0.4.exe` | Creates the install folder, Start Menu / Desktop shortcuts, and registers an uninstaller — no admin rights required. |
| 📦 **Portable build** | `ArtBayPublisher-v4.0.4-windows-x64.zip` | No installation — just unzip and run the `.exe` from anywhere (e.g. a USB drive). |

1. Run the installer (or unzip the portable build) and start `ArtBayPublisher.exe`.
2. Enter your Telegram Bot Token in the local dashboard and pair the owner account.
3. Add the `golden_key` cookie from your own FunPay account.
4. Send a ZIP/JSON package to the bot and confirm publishing.

> [!NOTE]
> Everything the app needs is compiled into the single `.exe` — no Python, Node.js, WebView2, or other runtime to install separately.

> [!TIP]
> Do not delete `%APPDATA%\ArtBayPublisher` during upgrades. It contains settings, queue checkpoints, and local backups.

## 📦 Package format

### Single offer

```text
my-lot.zip
├── lot.json
└── cover.png
```

```json
{
  "version": 2,
  "category_path": "Roblox Studio > Services",
  "title_ru": "💻 ROBLOX STUDIO | LUA / LUAU СКРИПТ",
  "title_en": "💻 ROBLOX STUDIO | LUA / LUAU SCRIPT",
  "description_ru": "Описание услуги",
  "description_en": "Service description",
  "payment_msg_ru": "Спасибо за покупку! Пришлите ТЗ.",
  "payment_msg_en": "Thank you! Please send your requirements.",
  "price": 199,
  "active": true,
  "image_files": ["cover.png"]
}
```

### Bulk archive

```text
bulk.zip
├── 0001_lot.zip
├── 0002_lot.zip
├── 0003_lot.zip
└── ...
```

Folder-based packages, shared `lots.json` / `artbay-batch.json` manifests, and `LOT_ID.png` image replacement packs are supported as well. See [`examples`](examples) for ready-to-edit manifests.

## 🤖 Telegram commands

| Command | Action |
|---|---|
| `/lots` | Show current offers |
| `/orders` | Show recent orders |
| `/stats` | Show statistics |
| `/categories` | Find a FunPay category |
| `/price ID 199` | Change one offer's price |
| `/prices -15%` | Apply a bulk price change |
| `/on ID` / `/off ID` | Enable or disable an offer |
| `/clone ID` | Clone an offer |
| `/export ID` | Export an offer as ZIP |
| `/delete ID` | Delete after creating a backup |
| `/rollback` | Roll back the last change |
| `/stop` | Pause the bulk queue |
| `/resume` | Resume a saved queue |
| `/cancel` | Permanently cancel and delete a queue |

## 🛡 Security

- The dashboard only listens on `127.0.0.1` and uses a local access key.
- Stored secrets are never rendered in HTML and are redacted from user-facing errors.
- ZIP extraction rejects path traversal and enforces size limits.
- Local backup records are created before destructive offer changes.
- This repository excludes user configs, tokens, cookies, logs, backups, and product packages.

See [`SECURITY.md`](SECURITY.md) for responsible reporting.

## 🧑‍💻 Build from source

Go 1.23+ is required.

```powershell
go test ./...

# Embed the app icon and version info (optional, done automatically in CI)
go install github.com/tc-hib/go-winres@latest
go-winres make

go build -trimpath -ldflags="-s -w -H=windowsgui" -o ArtBayPublisher.exe .

# Build the installer (optional, requires Inno Setup 6)
iscc installer\ArtBayPublisher.iss
```

## 🗺 Project status

- Current version: **4.0.4**
- Primary platform: **Windows 10/11 x64**
- Interface: **local web dashboard + Telegram**
- Changes: [`CHANGELOG.md`](CHANGELOG.md)

## 👨‍🚀 Developers

<table>
  <tr>
    <td align="center">
      <a href="https://github.com/RovelLabs"><b>RovelLabs</b></a><br>
      <sub>Development · architecture · maintenance</sub>
    </td>
  </tr>
</table>

## 📄 Legal

Copyright © 2026 RovelLabs. All rights reserved. See [`LICENSE`](LICENSE) for source-use terms.
