# Papi
Pinterest API

# Installation

```sh
git clone https://github.com/686udjie/Papi.git
cd Papi
go run .
```

# Usage
```pwsh
Pinterest API server

Usage:
  go run .

Endpoints:
  GET /api/pin
  GET /api/homefeed (auth required)
```

# Commands
## `GET /api/pin?id=<PIN_ID>`
```sh
Fetch Pinterest metadata by pin ID.

Example:
  curl "http://localhost:8080/api/pin?id=593701163419317900" | jq
```

## `GET /api/pin?url=<PIN_URL>`
```sh
Fetch Pinterest metadata by pin URL.

Example:
  curl "http://localhost:8080/api/pin?url=https://www.pinterest.com/pin/593701163419317900/" | jq
```

## `GET /api/homefeed`
```sh
Fetch the authenticated homefeed. Updates the bookmark cursor in the DB.

Environment:
  DATABASE_URL=postgres://user:pass@host:5432/dbname
  PINTEREST_AUTH_CONFIRMED=true

Example:
  curl "http://localhost:8080/api/homefeed" | jq
```

## `go run -tags=playwright .`
```sh
Starts the server and opens Playwright once to capture a session (if needed).
Skips capture on subsequent runs if a valid session exists.
```

# Homefeed Setup
## Schema
```sql
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  cookies_json TEXT NOT NULL,
  cookies_header TEXT NOT NULL,
  headers_json TEXT,
  user_agent TEXT,
  data_json TEXT,
  source_url TEXT,
  bookmark TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ
);
```

## Playwright Capture (standalone)
```sh
cd playwright
npm install
npx playwright install
npm run capture
```
The capture prints JSON with `cookies`, `cookies_header`, `bookmark`, and request headers. Insert those into the `sessions` table if not using the Playwright build tag.

# Responses
Successful response example:
```json
{
  "id": "593701163419317900",
  "type": "image",
  "url": "https://i.pinimg.com/originals/25/76/b3/2576b3a405691260be0527a1086a72c6.jpg",
  "filename": "pinterest_593701163419317900.jpg",
  "title": "No one can love as much as you",
  "description": "Jul 12, 2025 — An anime-style character with long black hair tied back, leaning against a wall with a \"No Smoking\" sign in both English and Japanese. The character is wearing a dark outfit and has their eyes closed.",
  "creator": "reaper"
}
```
`type` is one of: `image`, `video`, or `gif`.

Errors return JSON with HTTP status:
```json
{
  "error": "missing id or url"
}
```