<div align="center">
  <a href="https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-">
    <img src="docs/assets/banner.svg" alt="ArtBay Publisher Banner" width="100%">
  </a>

  <br><br>

  <p align="center">
    <a href="README.md"><b>🇷🇺 Русский</b></a>&nbsp;&nbsp;•&nbsp;&nbsp;
    <a href="README_EN.md"><b>🇬🇧 English</b></a>
  </p>

  <p align="center">
    <a href="https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-/releases/latest"><img src="https://img.shields.io/badge/Release-v4.0.4-6366f1?style=for-the-badge&logo=github&logoColor=white" alt="Release v4.0.4"></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.23+"></a>
    <img src="https://img.shields.io/badge/Platform-Windows%2010%20%7C%2011-0078D4?style=for-the-badge&logo=windows11&logoColor=white" alt="Windows 10/11">
    <img src="https://img.shields.io/badge/Telegram-Bot%20API-26A5E4?style=for-the-badge&logo=telegram&logoColor=white" alt="Telegram Bot">
    <img src="https://img.shields.io/badge/FunPay-Automation-ff5370?style=for-the-badge" alt="FunPay">
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-RovelLabs-10b981?style=for-the-badge" alt="License"></a>
  </p>

  <p align="center">
    <b>High-speed, resilient local-first bridge between Telegram and FunPay for intelligent bulk lot publishing and automated store management.</b>
  </p>
</div>

---

> [!IMPORTANT]
> **ArtBay Publisher** is an independent software tool created by **RovelLabs**. It is neither affiliated with nor an official product of FunPay or Telegram. Use automation responsibly and in accordance with platform policies.

---

## 📑 Table of Contents

- [✨ Key Features](#-key-features)
- [🧭 Architecture & Pipeline](#-architecture--pipeline)
- [🚀 Quick Start](#-quick-start)
- [🤖 Telegram Commands](#-telegram-commands)
- [📦 Package Formats & Manifests](#-package-formats--manifests)
- [🛡 Security & Local-First Design](#-security--local-first-design)
- [🧑‍💻 Build from Source](#-build-from-source)
- [👨‍🚀 Developers & Community](#-developers--community)
- [📄 License](#-license)

---

## ✨ Key Features

| Feature | Description & Benefits |
| :--- | :--- |
| 🚀 **High-Speed Bulk Publishing** | Publish hundreds of offers in minutes through an automated queue with preview validation and error mitigation. |
| ⏯ **Durable Queue Controls** | Resumable workflows via `/stop` (pause), `/resume` (continue), and `/cancel` (permanent cancel & checkpoint purge). |
| 🛡 **Anti-Duplicate & Limit Engine** | Smart duplicate detection and category capacity limit handling without halting the ongoing publishing queue. |
| 🔐 **100% Local-First & Zero-Leak** | Web dashboard runs strictly on `127.0.0.1:8765`. Telegram tokens and `golden_key` are encrypted and never exposed. |
| 🖼 **Smart Media Pipeline** | Native support for `PNG`, `JPG`, `WEBP`, `GIF` with SHA-256 image caching to prevent duplicate uploads. |
| 📦 **Flexible Ingestion Formats** | Handles single ZIPs, nested bulk ZIPs, `lots.json`, `artbay-batch.json`, and direct folder structures. |
| 🛍 **Full Lot Management** | Adjust prices (including percentage shifts `/prices -10%`), toggle active status, clone lots, export to ZIP, and rollback. |
| ⚡ **Fault-Tolerant Checkpoints** | State is saved to disk after every single lot processed — resume effortlessly right where you left off after restarts. |

---

## 🧭 Architecture & Pipeline

```mermaid
flowchart TD
    classDef client fill:#1e293b,stroke:#38bdf8,stroke-width:2px,color:#fff;
    classDef core fill:#1e1b4b,stroke:#818cf8,stroke-width:2px,color:#fff;
    classDef target fill:#3f0f1b,stroke:#fb7185,stroke-width:2px,color:#fff;
    classDef disk fill:#064e3b,stroke:#34d399,stroke-width:2px,color:#fff;

    A[📦 ZIP / JSON Packages] -->|Upload to Bot| B[📱 Telegram Bot UI]:::client
    B -->|Package Ingestion| C[⚡ ArtBay Core Engine<br>127.0.0.1:8765]:::core
    
    C -->|Queue Preparation| D{Preview & Integrity Check}:::core
    D -->|Confirmed| E[🚀 Publishing Queue Worker]:::core
    D -->|Invalid Package| F[⚠️ Telegram Error Alert]:::client

    E -->|Persistent State| G[(💾 Disk Checkpoint<br>%APPDATA%/ArtBayPublisher)]:::disk
    G -.->|Resume After Restart| E
    
    E -->|Anti-Dupe & Capacity Check| H[🌐 FunPay API Gateway]:::target
    H -->|Live Publication| I[✅ Active Lots Listed]:::target

    subgraph Queue Management
        J[/stop - Pause]
        K[/resume - Resume]
        L[/cancel - Cancel]
    end
    J -.-> E
    K -.-> E
    L -.-> E
```

---

## 🚀 Quick Start

### 1. Download Latest Release
Navigate to **[Releases](https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-/releases/latest)** and download:
```
ArtBayPublisher-v4.0.2-windows-x64.zip
```

### 2. Extract and Launch
1. Extract the ZIP archive to your preferred folder (e.g., `C:\ArtBayPublisher`).
2. Run **`UPDATE_AND_START.bat`** or **`ArtBayPublisher.exe`**.
3. The local management dashboard will automatically open in your browser: **`http://127.0.0.1:8765`**.

### 3. Setup Connection
1. **Telegram Bot Token**: Create a bot via [@BotFather](https://t.me/BotFather), paste the token into the dashboard.
2. **Owner Pairing**: Click the pair button or open the generated deep-link URL in your Telegram bot.
3. **FunPay golden_key**: Copy your account cookie `golden_key` from FunPay and save it in the dashboard.

### 4. Publish First Offer
- Drag and drop your product ZIP file directly into your Telegram bot chat.
- Review the interactive preview (title, price, description, images).
- Click **"Publish"** — your offer will appear live on FunPay!

> [!TIP]
> All application data, queue progress, and configuration are safely stored in `%APPDATA%\ArtBayPublisher`. This ensures zero data loss during version updates.

---

## 🤖 Telegram Commands

Control all publishing tasks and inventory management right from Telegram:

| Command | Example | Description |
| :--- | :--- | :--- |
| `/lots` | `/lots` | List all active and hidden offers |
| `/orders` | `/orders` | Display recent orders and transaction status |
| `/stats` | `/stats` | View sales metrics, store statistics, and balance |
| `/categories` | `/categories Roblox` | Search FunPay category IDs and taxonomy |
| `/price` | `/price 1234567 299` | Modify the price of a specific offer by ID |
| `/prices` | `/prices -10%` or `+50` | Bulk change prices across all active lots (% or fixed) |
| `/on` / `/off` | `/on 1234567` | Enable or disable offer visibility |
| `/clone` | `/clone 1234567` | Duplicate an existing offer with its assets |
| `/export` | `/export 1234567` | Export an offer package back into a portable ZIP archive |
| `/delete` | `/delete 1234567` | Delete an offer (with automated pre-deletion backup) |
| `/rollback` | `/rollback` | Undo recent offer edits or deletions |
| `/stop` | `/stop` | Pause active bulk publishing queue |
| `/resume` | `/resume` | Resume the paused bulk queue |
| `/cancel` | `/cancel` | Permanently cancel the queue and delete checkpoint |
| `/version` | `/version` | Show engine build version and service health |

---

## 📦 Package Formats & Manifests

### 📁 1. Single Lot Archive (`lot.zip`)
```text
my-awesome-product.zip
├── lot.json          # Manifest containing parameters & copy
└── cover.png         # Product cover image (PNG, JPG, WEBP, GIF)
```

Example `lot.json`:
```json
{
  "version": 2,
  "category_path": "Roblox Studio > Services",
  "title_ru": "💻 ROBLOX STUDIO | LUA / LUAU СКРИПТ ЛЮБОЙ СЛОЖНОСТИ",
  "title_en": "💻 ROBLOX STUDIO | LUA / LUAU CUSTOM SCRIPT",
  "description_ru": "Быстрая разработка скриптов и систем для ваших плейсов в Roblox Studio.",
  "description_en": "High-quality Roblox Studio scripting and Luau systems for your games.",
  "payment_msg_ru": "Спасибо за покупку! Пожалуйста, отправьте ТЗ в чат заказа.",
  "payment_msg_en": "Thank you for your purchase! Please describe your task in the order chat.",
  "price": 249,
  "active": true,
  "image_files": [
    "cover.png"
  ]
}
```

### 🗂 2. Bulk Multi-Lot Package (`bulk.zip`)
```text
bulk-upload.zip
├── 001_lot.zip
├── 002_lot.zip
├── 003_lot.zip
└── ...
```

---

## 🛡 Security & Local-First Design

- 🔒 **Loopback Only**: The dashboard is bound strictly to `127.0.0.1:8765` with zero external ingress.
- 🔑 **Redacted Storage**: Telegram credentials and FunPay cookies are safely persisted and never rendered in browser markup.
- 🗜 **Zip Slip Prevention**: All uploaded archives undergo rigorous path traversal and expansion size checks.
- 💾 **Automatic Snapshots**: Snapshot records are created prior to destructive operations, allowing one-click rollback (`/rollback`).

---

## 🧑‍💻 Build from Source

Requires **Go 1.23+**:

```powershell
# 1. Clone repository
git clone https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-.git
cd ArtBay-Publisher-FunPay-bot-

# 2. Run unit tests and static checks
go test -v ./...
go vet ./...

# 3. Build optimized Windows GUI binary
go build -trimpath -ldflags="-s -w -H=windowsgui" -o ArtBayPublisher.exe .
```

---

## 👨‍🚀 Developers & Community

<table align="center">
  <tr>
    <td align="center" width="220">
      <a href="https://github.com/RovelLabs">
        <img src="https://github.com/RovelLabs.png" width="100px;" alt="RovelLabs"/><br />
        <sub><b>RovelLabs</b></sub>
      </a><br />
      <sub>Architecture · Core Engine · UI</sub>
    </td>
  </tr>
</table>

- **GitHub Organization**: [@RovelLabs](https://github.com/RovelLabs)
- **Repository**: [ArtBay-Publisher-FunPay-bot-](https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-)
- **Issues & Suggestions**: [Open an Issue](https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-/issues)

---

## 🏷️ Tags & Keywords

`funpay` • `funpay-bot` • `telegram-bot` • `funpay-publisher` • `automation` • `golang` • `go` • `marketplace` • `bulk-uploader` • `ecommerce` • `windows` • `local-first` • `lot-manager` • `funpay-auto-response` • `auto-publishing`

---

## 📄 License

Copyright © 2026 **RovelLabs**. All rights reserved.  
Distributed under the terms of the [Source-Available License](LICENSE).
