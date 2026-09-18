# WhatsDown

**WHatsDown (Personal WhatsApp Terminal)** is a Go CLI for controlling and interacting with your personal WhatsApp account directly from the terminal.

It uses [`go.mau.fi/whatsmeow`](https://pkg.go.dev/go.mau.fi/whatsmeow) to connect to WhatsApp through the WhatsApp Web/Multidevice protocol.

The goal is to make common WhatsApp actions available through a clean developer-friendly CLI:

```powershell
pstw send Pratyush "Bring me chocolate"
pstw send 8005331766 "Bring me chocolate"

pstw chats
pstw messages Pratyush
pstw contacts
pstw check 8005331766

pstw reply Pratyush "yeah sure"
pstw reply Pratyush 3 "yeah sure"

pstw send Pratyush --file .\resume.pdf
```

PSTW is intended for personal use with the user's own WhatsApp account.

> **Important:** `whatsmeow` is an unofficial WhatsApp client library. Using unofficial clients may violate WhatsApp's terms and can result in account restrictions. Do not use PSTW for spam, bulk messaging, mass automation, or abuse.

---

## Features

PSTW should provide a unified terminal interface for:

### Messaging

- Send text messages.
- Send messages using a contact name.
- Send messages directly using a phone number.
- Send documents/files.
- Send images, videos, and other supported media.
- Reply to messages.
- Reply to the latest message in a chat.
- Reply to a specific message.
- React to messages.
- Support private chats and groups where supported by `whatsmeow`.

### Contacts

- List contacts.
- Search contacts by name.
- Resolve a contact name to a WhatsApp JID.
- Check whether a phone number is registered on WhatsApp.
- Display useful contact information.

Example:

```powershell
pstw contacts
pstw contacts Pratyush
pstw check 8005331766
```

### Chats

- List recent chats.
- Show the last 5 chats.
- Open/read messages from a chat.
- Show recent messages.
- Identify incoming vs outgoing messages.
- Keep enough local message metadata to make replies and searches convenient.

Example:

```powershell
pstw chats
pstw messages Pratyush
```

### Replies

PSTW should make replying extremely simple.

Instead of manually finding a WhatsApp message ID:

```powershell
pstw reply Pratyush "yeah sure"
```

should reply to the latest relevant message in that chat.

For explicit control:

```powershell
pstw reply Pratyush 3 "yeah sure"
```

where `3` is the local message reference shown by:

```powershell
pstw messages Pratyush
```

The actual WhatsApp reply should use the appropriate WhatsApp message context/reference supported by `whatsmeow`.

### Media

PSTW should support sending and receiving supported WhatsApp media.

Examples:

```powershell
pstw send Pratyush --file .\resume.pdf
pstw send Pratyush --image .\photo.jpg
pstw send Pratyush --video .\video.mp4
```

Received media should be downloadable through PSTW.

---

# Architecture

The application should remain a single Go application.

```text
                         ┌──────────────────────┐
                         │       PSTW CLI        │
                         │      Cobra / Go       │
                         └──────────┬───────────┘
                                    │
                 ┌──────────────────┼──────────────────┐
                 │                  │                  │
                 ▼                  ▼                  ▼
          ┌────────────┐     ┌────────────┐     ┌────────────┐
          │ Messaging  │     │ Contacts   │     │   Chats    │
          └─────┬──────┘     └─────┬──────┘     └─────┬──────┘
                │                  │                  │
                └──────────────────┼──────────────────┘
                                   ▼
                         ┌───────────────────┐
                         │    whatsmeow      │
                         │ WhatsApp Client   │
                         └─────────┬─────────┘
                                   │
                                   ▼
                              WhatsApp
```

Local persistence:

```text
                    ┌──────────────────────┐
                    │       Database       │
                    ├──────────────────────┤
                    │ whatsmeow session    │
                    └──────────────────────┘
```

There should be a clear distinction between:

1. **whatsmeow's device/session store**
2. **PSTW's application database**

Do not modify the whatsmeow session tables directly.

---

# Requirements

## Software

- Go 1.22+ recommended.
- A WhatsApp account.
- WhatsApp installed on a phone capable of linking devices.
- Windows/macOS/Linux supported by Go and the selected SQLite driver.

Check Go:

```powershell
go version
```

---

# Installation

## 1. Create the project

```powershell
mkdir pstw
cd pstw

go mod init github.com/Pratyush-who/pstw
```

Replace the module path if the GitHub repository uses another path.

---

## 2. Install whatsmeow

```powershell
go get go.mau.fi/whatsmeow
```

Primary documentation:

https://pkg.go.dev/go.mau.fi/whatsmeow

Repository:

https://github.com/tulir/whatsmeow

---

## 3. Install SQLite support

Use the SQL store provided by whatsmeow with SQLite.

Install the SQLite driver required by the selected SQL-store setup:

```powershell
go get github.com/mattn/go-sqlite3
```

If the current whatsmeow documentation recommends a different SQLite driver/setup for the installed version, follow the current documentation rather than an outdated example.

---

## 4. Install terminal QR support

```powershell
go get github.com/mdp/qrterminal/v3
```

This is used to display the WhatsApp pairing QR code directly in the terminal.

---

## 5. CLI dependency

Use Cobra for the command structure:

```powershell
go get github.com/spf13/cobra
```

Then:

```powershell
go mod tidy
```

---

# Project Structure

Use a simple structure that separates the CLI from the WhatsApp integration.

```text
pstw/
│
├── cmd/
│   └── pstw/
│       └── main.go
│
├── internal/
│   ├── whatsapp/
│   │   ├── client.go
│   │   ├── auth.go
│   │   ├── messages.go
│   │   ├── contacts.go
│   │   ├── chats.go
│   │   ├── media.go
│   │   └── events.go
│   │
│   ├── db/
│   │   ├── db.go
│   │   ├── messages.go
│   │   ├── contacts.go
│   │   └── chats.go
│   │
│   └── cli/
│       ├── root.go
│       ├── send.go
│       ├── reply.go
│       ├── chats.go
│       ├── messages.go
│       ├── contacts.go
│       ├── media.go
│       └── status.go
│
├── data/
├── downloads/
│
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

Do not introduce unnecessary microservices, HTTP servers, Redis, Kafka, or other infrastructure.

PSTW should remain a focused Go CLI.

---

# WhatsApp Authentication

PSTW uses whatsmeow's device store to maintain a WhatsApp Web/Multidevice session.

The first run should create a local device.

Conceptually:

```text
PSTW
 │
 ▼
Initialize SQLite
 │
 ▼
Create/Get whatsmeow device
 │
 ▼
Create whatsmeow Client
 │
 ├── Existing device?
 │       │
 │       └── Connect
 │
 └── New device?
         │
         ▼
      Create QR channel
         │
         ▼
      Connect
         │
         ▼
      Display QR
         │
         ▼
      Scan using WhatsApp
         │
         ▼
      Authentication succeeds
         │
         ▼
      Session persisted
```

Follow the current `whatsmeow` documentation/API for the exact implementation because the library can change over time.

---

# Pairing

Run:

```powershell
go run ./cmd/pstw
```

or, once the CLI command is implemented:

```powershell
pstw setup
```

On the first run, PSTW should detect that the device is not paired.

Display:

```text
PSTW

No WhatsApp session found.

Open WhatsApp on your phone:

WhatsApp
→ Settings
→ Linked Devices
→ Link a Device

Scan the QR code below.

[ QR CODE ]

Waiting for authentication...
```

After scanning:

```text
Authentication successful.
WhatsApp connected ✓
```

The application should remain running while connected.

---

# Persistent Session

The most important authentication requirement is that the user should **not need to scan the QR code every time**.

After the first successful pairing:

```text
data/
└── whatsapp.db
```

contains the local persistent device/session state.

On the next launch:

```powershell
pstw
```

PSTW should:

```text
Existing WhatsApp session found.
Connecting...

Connected ✓
```

without showing another QR code.

A QR should only be required when the account/device needs to be paired again.

---

# WhatsApp Client

Create one central WhatsApp client that is shared by the application.

Conceptually:

```go
store
  ↓
device
  ↓
whatsmeow.Client
  ↓
PSTW services/commands
```

Avoid creating a new WhatsApp client for every command.

The application should initialize one client and reuse it.

---

# Database

PSTW needs local persistence for both the WhatsApp session and application-level information.

## WhatsApp session

Managed by:

```text
whatsmeow
    ↓
sqlstore
    ↓
SQLite
```

Do not manually manipulate these tables.

## PSTW database

PSTW can maintain its own tables for convenient local functionality.

Example:

```sql
CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    name TEXT,
    last_message_id TEXT,
    last_message_at INTEGER
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    chat_jid TEXT NOT NULL,
    sender_jid TEXT,
    sender_name TEXT,
    text TEXT,
    timestamp INTEGER,
    from_me BOOLEAN,
    reply_to TEXT,
    media_type TEXT,
    media_path TEXT
);
```

These are conceptual schemas. Adapt them to the actual implementation.

---

# Message Model

Every received/sent message should have a stable WhatsApp message ID.

PSTW should also assign a small local display index when showing messages.

Example:

```text
Pratyush Mehra

[1] You · 10:31
    Bro where are you?

[2] Pratyush · 10:32
    Coming

[3] You · 10:33
    Bring chocolate

[4] Pratyush · 10:34
    😂
```

This allows:

```powershell
pstw reply Pratyush 4 "and get some chips"
```

The local `4` is only a CLI convenience.

Internally PSTW must resolve it to the real WhatsApp message ID and sender/context before creating the reply.

---

# Sending Messages

The fundamental command is:

```powershell
pstw send <recipient> "<message>"
```

Examples:

```powershell
pstw send Pratyush "Bring me chocolate"
```

```powershell
pstw send 8005331766 "Bring me chocolate"
```

The recipient resolver should determine whether the input is:

- A contact name.
- A phone number.
- A known JID.
- A group/chat identifier.

Phone numbers should be normalized appropriately before constructing/resolving the WhatsApp JID.

For simple text messages, use the message structure expected by the current whatsmeow version.

---

# Recipient Resolution

All commands that target a chat should use the same resolver.

Conceptually:

```text
resolveRecipient("Pratyush")
             │
             ├── known local contact
             │
             ├── WhatsApp contact
             │
             ├── phone number
             │
             └── JID
```

This prevents each command from implementing its own contact lookup logic.

For example:

```powershell
pstw send Pratyush "hello"
pstw reply Pratyush "sure"
pstw messages Pratyush
```

should all resolve `Pratyush` consistently.

---

# Contacts

Command:

```powershell
pstw contacts
```

Example:

```text
CONTACTS

Pratyush Mehra       +91 8005331766
Rahul Sharma         +91 9876543210
Ankit                +91 9123456780
```

Search:

```powershell
pstw contacts Pratyush
```

Example:

```text
1 result

Pratyush Mehra
+91 8005331766
WhatsApp: yes
```

---

# WhatsApp Number Check

Support:

```powershell
pstw check 8005331766
```

Expected output:

```text
+91 8005331766

WhatsApp: YES
```

or:

```text
+91 8005331766

WhatsApp: NO
```

The implementation should use the current whatsmeow API for checking whether the number can be resolved/queried on WhatsApp.

Do not fake this result based only on whether the number exists in the local phone contacts.

---

# Recent Chats

Support:

```powershell
pstw chats
```

The default view should show approximately the latest 5 chats.

Example:

```text
RECENT CHATS

1. Pratyush Mehra      2 min ago
2. KIET Group          18 min ago
3. Rahul               1 hr ago
4. Mom                 3 hr ago
5. Innogeeks           yesterday
```

The list should be based on actual available WhatsApp chat/message information.

Do not invent chats from the local database if WhatsApp data is unavailable.

---

# Reading Messages

Support:

```powershell
pstw messages Pratyush
```

Default behavior should show a useful recent window, such as the latest 5 or 10 messages.

Example:

```text
Pratyush Mehra

[1] You · 10:31
    Bro where are you?

[2] Pratyush · 10:32
    Coming

[3] You · 10:33
    Bring chocolate

[4] Pratyush · 10:34
    😂
```

The exact number can be configurable later, but the default should remain concise.

---

# Replies

Support two forms.

## Reply to latest message

```powershell
pstw reply Pratyush "yeah sure"
```

PSTW should find the appropriate latest message and construct a WhatsApp reply referencing it.

## Reply using local message ID

```powershell
pstw reply Pratyush 3 "yeah sure"
```

Flow:

```text
Pratyush
   │
   ▼
Local message #3
   │
   ▼
Real WhatsApp message ID
   │
   ▼
Message sender + chat context
   │
   ▼
WhatsApp ContextInfo
   │
   ▼
whatsmeow
   │
   ▼
Reply sent
```

Do not merely send a new standalone message.

It must be an actual WhatsApp reply/quoted-message reference.

---

# File Sending

Support:

```powershell
pstw send Pratyush --file .\resume.pdf
```

The flow should be:

```text
Local file
   │
   ▼
Read file
   │
   ▼
Determine MIME/type
   │
   ▼
Upload using whatsmeow
   │
   ▼
Create appropriate WhatsApp media message
   │
   ▼
Send
```

Example output:

```text
Uploading resume.pdf...
Sending to Pratyush...

✓ File sent
```

The same mechanism should be extensible to supported media types.

---

# Media

Where supported by the current whatsmeow API, PSTW should handle:

```text
Documents
Images
Videos
Audio
Other supported WhatsApp media
```

Commands may use explicit flags:

```powershell
pstw send Pratyush --file .\resume.pdf
pstw send Pratyush --image .\photo.jpg
pstw send Pratyush --video .\video.mp4
```

The implementation should avoid duplicating upload logic across commands.

Create a common media/upload layer.

---

# Receiving Messages

Register a whatsmeow event handler.

Incoming message flow:

```text
WhatsApp
   │
   ▼
whatsmeow event
   │
   ▼
PSTW event handler
   │
   ├── identify chat
   ├── identify sender
   ├── extract text
   ├── identify media
   ├── identify reply context
   └── persist metadata
```

Received messages should be stored in the PSTW database so commands such as:

```powershell
pstw messages Pratyush
```

and:

```powershell
pstw reply Pratyush 4 "sure"
```

can work efficiently.

---

# Live Listener

Provide:

```powershell
pstw listen
```

Example:

```text
PSTW listening...

[10:42] Pratyush
Bro where are you?

[10:43] Mom
Call me when free.

[10:44] Rahul
Check this out
```

The listener should consume actual whatsmeow events.

It should not poll WhatsApp continuously.

Use the event system exposed by whatsmeow.

---

# Reactions

Support message reactions where the current whatsmeow API allows them.

Example:

```powershell
pstw react Pratyush 4 "👍"
```

The local message reference should resolve to the actual WhatsApp message ID and appropriate sender/chat context.

---

# Read / Typing State

Where supported by the current whatsmeow API, PSTW can expose:

```powershell
pstw typing Pratyush
```

and appropriate read/receipt operations.

These should use whatsmeow's existing protocol functionality rather than implementing custom polling.

---

# Search

PSTW should eventually be able to search locally indexed messages:

```powershell
pstw search "chocolate"
```

Example:

```text
SEARCH: chocolate

Pratyush Mehra · Today 10:33
Bring chocolate

Mom · Yesterday 18:22
Don't forget the chocolate.
```

Search should operate against PSTW's local message database.

Do not repeatedly query WhatsApp for every search.

---

# Command Reference

The CLI should expose a consistent command structure.

```text
pstw setup
pstw status

pstw send <recipient> "<message>"
pstw send <recipient> --file <path>
pstw send <recipient> --image <path>
pstw send <recipient> --video <path>

pstw reply <chat> "<message>"
pstw reply <chat> <message-number> "<message>"

pstw contacts
pstw contacts <search>
pstw check <phone-number>

pstw chats
pstw messages <chat>

pstw react <chat> <message-number> "<emoji>"

pstw download <message-id>

pstw search "<text>"
pstw listen
```

The CLI can evolve, but commands should remain predictable.

---

# Status

Support:

```powershell
pstw status
```

Example:

```text
PSTW

WhatsApp
────────
Status: Connected
Account: +91 XXXXX XXXXX
Session: Active
```

If disconnected:

```text
PSTW

WhatsApp
────────
Status: Disconnected
```

If no account is paired:

```text
PSTW

No WhatsApp account is linked.

Run:

    pstw setup
```

---

# Error Handling

CLI errors should be understandable.

Bad recipient:

```text
✗ Contact not found: Pratyush

Try:

    pstw contacts Pratyush
```

Not connected:

```text
✗ WhatsApp is not connected.

Run:

    pstw setup
```

File does not exist:

```text
✗ File not found:

    .\resume.pdf
```

Number unavailable:

```text
✗ This number is not available on WhatsApp:

    +91 8005331766
```

Do not expose huge low-level protocol errors to normal users.

During development, retain useful wrapped errors/logging.

---

# Logging

Use structured, useful logs for development.

Do not log:

- Authentication secrets.
- Session credentials.
- Sensitive message content unnecessarily.
- Raw session database contents.

Normal CLI output should be clean.

Debug logging can be enabled separately if required.

---

# Security

The WhatsApp session database is sensitive.

Never commit:

```text
data/
*.db
*.db-shm
*.db-wal
```

Never upload or share the session database.

Recommended `.gitignore`:

```gitignore
data/
*.db
*.db-shm
*.db-wal

downloads/

.env

bin/
*.exe
```

Do not hard-code credentials or session information.

---

# Development

Format:

```powershell
go fmt ./...
```

Test:

```powershell
go test ./...
```

Tidy:

```powershell
go mod tidy
```

Run:

```powershell
go run ./cmd/pstw
```

Build:

```powershell
go build -o pstw.exe ./cmd/pstw
```

Run the compiled CLI:

```powershell
.\pstw.exe
```

---

# Implementation Rules

## Keep it Go

The entire application should be written in Go.

Do not introduce:

- Node.js
- TypeScript
- Baileys
- whatsapp-web.js
- Python bridges
- External WhatsApp gateway services

`whatsmeow` is the WhatsApp integration.

---

## Use Current whatsmeow APIs

Always prefer the current API/documentation:

https://pkg.go.dev/go.mau.fi/whatsmeow

Do not blindly copy code from old GitHub examples.

If an API has changed, adapt the implementation to the installed version.

---

## Keep the WhatsApp Client Centralized

There should be one main `whatsmeow.Client`.

Other components should use it rather than creating their own clients.

---

## Keep Recipient Resolution Centralized

Do not implement separate name/number/JID resolution logic inside:

```text
send
reply
messages
media
react
```

Create one reusable resolver.

---

## Keep Message References Centralized

A local message number such as:

```text
[4]
```

must resolve to the real WhatsApp message metadata.

The reply/reaction system should use the same message lookup mechanism.

---

## Avoid Overengineering

This is a personal CLI, not a distributed production platform.

Do not add infrastructure unless it solves an actual problem.

Preferred:

```text
Go
+
whatsmeow
+
SQLite
+
Cobra
```

Keep the code readable and maintainable.

---

# Example User Experience

A normal session should eventually look like:

```text
PS C:\> pstw status

PSTW
WhatsApp: Connected
Account: +91 XXXXX XXXXX


PS C:\> pstw chats

RECENT CHATS

1. Pratyush Mehra      2 min ago
2. Mom                 18 min ago
3. Rahul               1 hr ago
4. Innogeeks           3 hr ago
5. KIET Group          yesterday


PS C:\> pstw messages Pratyush

Pratyush Mehra

[1] You · 10:31
    Bro where are you?

[2] Pratyush · 10:32
    Coming

[3] You · 10:33
    Bring chocolate

[4] Pratyush · 10:34
    😂


PS C:\> pstw reply Pratyush 4 "and get some chips"

↩ Replying to: 😂

✓ Message sent


PS C:\> pstw send Pratyush "Don't forget the chocolate"

✓ Message sent


PS C:\> pstw send Pratyush --file .\resume.pdf

Uploading resume.pdf...
✓ File sent
```

The goal is for the CLI to feel this simple even though the underlying implementation handles JIDs, events, device sessions, protobuf messages, media uploads, message context, and local persistence.

---

# Data Layout

Recommended local layout:

```text
pstw/
│
├── data/
│   ├── whatsapp.db
│   └── pstw.db
│
└── downloads/
    ├── Pratyush/
    ├── Rahul/
    └── ...
```

`whatsapp.db` is controlled by the whatsmeow SQL store.

`pstw.db` is controlled by PSTW.

`downloads/` contains downloaded WhatsApp media.

---

# Definition of Done

PSTW should be considered complete when a user can:

### Authentication

```powershell
pstw setup
```

→ scan QR

→ connect

→ restart PSTW

→ reconnect without scanning again.

### Messaging

```powershell
pstw send Pratyush "Bring me chocolate"
```

sends a real WhatsApp message.

### Contacts

```powershell
pstw contacts
pstw contacts Pratyush
pstw check 8005331766
```

provide useful contact/WhatsApp information.

### Chats

```powershell
pstw chats
```

shows recent chats.

### Messages

```powershell
pstw messages Pratyush
```

shows recent messages with local references.

### Replies

```powershell
pstw reply Pratyush "sure"
```

replies to the latest message.

```powershell
pstw reply Pratyush 3 "sure"
```

replies to the selected message.

### Files

```powershell
pstw send Pratyush --file .\resume.pdf
```

sends the file through WhatsApp.

### Incoming events

```powershell
pstw listen
```

shows incoming messages and persists useful message metadata.

### Local search

```powershell
pstw search "chocolate"
```

searches locally indexed messages.

---

# References

## whatsmeow

Documentation:

https://pkg.go.dev/go.mau.fi/whatsmeow

GitHub:

https://github.com/tulir/whatsmeow

The implementation should treat the current whatsmeow documentation and source as the authority for API usage.

---

# License

Choose an appropriate license for the PSTW project.

Note that `whatsmeow` itself is licensed separately; review its license before distributing PSTW.
