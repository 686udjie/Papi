# Papi
Pinterest API

# Installation

```sh
git clone https://github.com/686udjie/Papi.git
cd Papi
go run .
```

# Setup
## Common Environment
These variables are shared by the authenticated endpoints.

```env
DATABASE_URL=postgres://user:pass@host:5432/dbname
PINTEREST_AUTH_CONFIRMED=true
PINTEREST_EMAIL=you@example.com
PINTEREST_PASSWORD=your_password
```

Notes:
- `POST /api/login` needs `DATABASE_URL`
- `GET /api/homefeed`, `GET /api/search`, and `GET /api/board` use the full authenticated setup above
- `PINTEREST_AUTH_CONFIRMED` is auto-enabled when running via `go run .`
- if `PINTEREST_EMAIL` and `PINTEREST_PASSWORD` are set, authenticated endpoints can auto-refresh the stored session without manually calling `/api/login`

## Session Schema
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

## Run
```sh
go run .
```

# Endpoints
```text
GET /api/pin
POST /api/login
GET /api/homefeed
GET /api/search
GET /api/board
```

# Usage
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

Example:
  curl "http://localhost:8080/api/homefeed" | jq
```

## `POST /api/login`
```sh
Authenticate and store a session using a static HTTP login flow.

Example:
  curl -X POST "http://localhost:8080/api/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"you@example.com","password":"your_password"}' | jq
```

## `GET /api/search?q=<QUERY>&rs=<SOURCE>`
```sh
Fetch extracted pin objects from a Pinterest search results page.

Example:
  curl "http://localhost:8080/api/search?q=hello&rs=typed" | jq
```

## `GET /api/board?url=<BOARD_URL>`
```sh
Fetch a board and all of its child sections, including extracted pins for each section.

Example:
  curl "http://localhost:8080/api/board?url=https://www.pinterest.com/Skatbad07/inspo/" | jq
```

# Session Setup
Use `POST /api/login` to create or overwrite the session row in the `sessions` table.

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
