# Pinterest API

## Run

```bash
git clone https://github.com/686udjie/Papi.git
cd Papi
go run .
```

Server starts on `:8080`.

## Use

Request by pin URL:

```bash
curl "http://localhost:8080/api/pin?url=https://www.pinterest.com/pin/593701163419317900/" | jq
```

Or by pin ID:

```bash
curl "http://localhost:8080/api/pin?id=593701163419317900" | jq
```

## Response

Successful response:

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

## Errors

Errors return JSON with HTTP status:

```json
{
  "error": "missing id or url"
}
```