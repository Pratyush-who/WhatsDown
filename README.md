# WhatsDown (PSTW)

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/WhatsApp-MultiDevice-25D366?style=for-the-badge&logo=whatsapp&logoColor=white" alt="WhatsApp Web" />
  <img src="https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Linux-blue?style=for-the-badge" alt="Platforms" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
</p>

<p align="center">
  <strong>WhatsDown</strong> is a fast, terminal-native WhatsApp client and automation CLI built in Go with <a href="https://github.com/tulir/whatsmeow">whatsmeow</a>. Send texts and media files, inspect chat history, and schedule future message deliveries with a persistent background worker.
</p>

---

## ✨ Features

- 💬 **Direct & Contact Messaging**: Send text messages by phone number (`8005331766`) or saved contact name (`"Yash Chatrath"`).
- 📸 **Rich Media Transmission**: Automatic MIME type detection for **Images** (`.png`, `.jpg`), **Videos** (`.mp4`), **Audio** (`.mp3`, `.ogg`), and **Documents** (`.pdf`, `.zip`, `.docx`).
- ⏰ **Offline Message Scheduler**: Schedule messages and media (`--at "15-10-2026 00:00"` or `--in 2h`). Queue is persisted to disk and delivered automatically via the background daemon even after terminal close.
- 🔄 **Missed Schedule Catch-Up**: Overdue messages are automatically delivered upon the next reconnect.
- 🔍 **Chat History & Inspection**: Inspect previous messages (`YOU -->` / `<-- Contact`) from local session storage and on-demand WhatsApp history sync.
- ⚡ **Interactive Prompt & CLI**: Use direct CLI commands (`.\pstw.exe send ...`) or work in a fast interactive REPL (`>>`).
- 🎯 **Shell Autocompletion**: Tab completion for PowerShell, Bash, Zsh, and Fish with dynamic contact and schedule ID suggestions.

---

## 🚀 Quick Start

### 1. Build
```powershell
go build -o pstw.exe ./cmd/pstw
```

### 2. Pair WhatsApp Account
Run PSTW to generate a QR code in the terminal:
```powershell
.\pstw.exe setup
```
Open **WhatsApp on your phone** $\rightarrow$ **Settings** $\rightarrow$ **Linked Devices** $\rightarrow$ **Link a Device** $\rightarrow$ Scan the QR code displayed in your terminal.

> 🔒 **Session Persistence**: Credentials and encryption keys are stored securely in `data/whatsapp.db`. You do **not** need to re-scan the QR code on every launch.

---

## 💻 Usage

WhatsDown can be used in two ways: **Interactive Shell Mode** (`>>`) or **Direct CLI Subcommands**.

### 1. Interactive Shell Mode

Run `.\pstw.exe` or `go run ./cmd/pstw` without arguments to enter the interactive shell:

```text
Personal WhatsApp CLI
Type help to explore the features.

WhatsApp connected.
Account: 918005331766:80@s.whatsapp.net

>> send 8005331766 Hey! How are you?
Message sent.

>> media "Yash Chatrath" .\presentation.pdf Project overview
Media sent.

>> schedule 8005331766 "Happy Birthday!" --at "15-10-2026 00:00"
Message scheduled successfully!
ID:           sch_42109_9f3a
Scheduled At: 2026-10-15 00:00:00 (423h45m from now)

>> inspect "Yash Chatrath" 5
[2026-09-27 12:00] YOU -->
    Event starts on the 10th
[2026-09-27 12:24] Yash Chatrath <--
    Confirmed for the 10th!

>> quit
```

---

### 2. Direct CLI Subcommands

#### 💬 Messaging & Media
```powershell
# Send a text message to a phone number or contact
.\pstw.exe send 8005331766 "See you at 5!"
.\pstw.exe send "Yash Chatrath" "Here is the update"

# Send media (Photo, Video, Audio, or Document) with optional caption
.\pstw.exe media 8005331766 .\photo.png "Look at this"
.\pstw.exe media "Yash Chatrath" .\document.pdf
```

#### ⏰ Scheduling Messages
```powershell
# Schedule with absolute date/time (DD-MM-YYYY HH:MM or YYYY-MM-DD HH:MM)
.\pstw.exe schedule "Yash Chatrath" "Happy Birthday!" --at "15-10-2026 00:00"
.\pstw.exe schedule 8005331766 "Good morning!" --at "tomorrow 09:00"

# Schedule with relative duration
.\pstw.exe schedule 8005331766 "Meeting in 15 mins" --in 15m
.\pstw.exe schedule 8005331766 "Follow up with team" --in 2h

# Schedule media file delivery
.\pstw.exe schedule 8005331766 --file .\gift.png --caption "Surprise!" --at "15-10-2026 00:00"

# List and cancel schedules
.\pstw.exe schedule list
.\pstw.exe schedule list --all
.\pstw.exe schedule cancel sch_42109_9f3a
```

#### 🔍 Chats & History Inspection
```powershell
# Show recently active chats
.\pstw.exe chats

# Inspect latest messages in a chat (default: 5 messages, or custom count)
.\pstw.exe inspect 8005331766
.\pstw.exe inspect "Yash Chatrath" 20
```

#### 🤖 Background Daemon Mode
To send scheduled messages on time even when you close the interactive prompt:

```powershell
# 1. Run in current terminal:
.\pstw.exe daemon

# 2. Run silently in Windows background (close terminal safely):
Start-Process -NoNewWindow -FilePath ".\pstw.exe" -ArgumentList "daemon"
```

---

## ⚡ Tab Autocompletion

WhatsDown provides dynamic autocompletion for subcommands, flags, contact names, schedule IDs, and file paths.

### PowerShell (Current Session)
```powershell
.\pstw.exe completion powershell | Out-String | Invoke-Expression
```

### PowerShell (Permanent Setup)
```powershell
Add-Content -Path $PROFILE -Value ".\pstw.exe completion powershell | Out-String | Invoke-Expression"
```

### Bash
```bash
source <(./pstw completion bash)
```

### Zsh
```zsh
source <(./pstw completion zsh)
```

### Fish
```fish
./pstw completion fish | source
```

---

## 📋 Command Reference

| Command | Type | Description | Example |
| :--- | :--- | :--- | :--- |
| `setup` | CLI & Interactive | Connect or pair a new WhatsApp account with QR code | `.\pstw.exe setup` |
| `status` | CLI & Interactive | Check connection and account JID status | `.\pstw.exe status` |
| `send` | CLI & Interactive | Send a text message to a contact or phone number | `send 8005331766 "Hello"` |
| `media` | CLI & Interactive | Send a photo, video, audio file, or document | `media 8005331766 .\doc.pdf` |
| `schedule` | CLI & Interactive | Schedule a future message or media file | `schedule 8005331766 "Hi" --in 2h` |
| `schedules` | Interactive | List pending and completed scheduled messages | `schedules` |
| `cancel` | Interactive | Cancel a scheduled message by ID | `cancel sch_42109_9f3a` |
| `inspect` | CLI & Interactive | View conversation history with sent/received flow | `inspect "Yash" 10` |
| `chats` | CLI & Interactive | List recently active chats | `chats` |
| `daemon` | CLI | Run headless background worker for schedule delivery | `.\pstw.exe daemon` |
| `completion` | CLI | Output shell autocompletion script | `.\pstw.exe completion powershell` |
| `quit` | Interactive | Disconnect session and exit shell | `quit` |

---

## 🏗️ Architecture

```text
┌────────────────────────────────────────────────────────┐
│                   WhatsDown CLI (pstw)                 │
│              Cobra / Interactive Readline              │
└───────────┬────────────────────────────────┬───────────┘
            │                                │
            ▼                                ▼
┌───────────────────────┐        ┌───────────────────────┐
│  WhatsApp Controller  │        │   Message Scheduler   │
│  - Contacts Search    │        │   - Time Parser       │
│  - Media Formatter    │        │   - Background Daemon │
│  - History Sync       │        │   - State Store       │
└───────────┬───────────┘        └───────────┬───────────┘
            │                                │
            └────────────────┬───────────────┘
                             ▼
                 ┌───────────────────────┐
                 │       whatsmeow       │
                 │   E2EE Signal Client  │
                 └───────────┬───────────┘
                             │
            ┌────────────────┴────────────────┐
            ▼                                 ▼
┌───────────────────────┐         ┌───────────────────────┐
│   SQLite Session DB   │         │    WhatsApp Servers   │
│   (data/whatsapp.db)  │         │  (Encrypted WebSocket)│
└───────────────────────┘         └───────────────────────┘
```

---

## 🔒 Security & Privacy

- All encryption keys and session credentials remain securely stored in your local `data/` directory.
- Direct communication with official WhatsApp servers via end-to-end encrypted TLS WebSockets.
- No third-party servers or telemetry.

---

## 📄 License

MIT © [Pratyush](https://github.com/Pratyush-who)
