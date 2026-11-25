import hashlib
import os
from datetime import datetime, timezone
from typing import Any

import psycopg
import uvicorn
from fastapi import FastAPI, HTTPException, Request
from psycopg.rows import dict_row
from psycopg.types.json import Jsonb
from pydantic import BaseModel, Field


class EntryInput(BaseModel):
    key: str = Field(min_length=1, max_length=120, pattern=r"^[A-Za-z0-9._-]+$")
    value: Any


app = FastAPI(title="Soha App Hosting Python starter", docs_url=None, redoc_url=None, openapi_url=None)


def decode_header(value: str | None) -> str:
    if not value:
        return ""
    try:
        return value.encode("latin-1").decode("utf-8").strip()
    except UnicodeError:
        return value.strip()


def read_identity(request: Request) -> dict[str, str]:
    user_id = decode_header(request.headers.get("x-soha-user-id"))
    if not user_id or len(user_id) > 200 or any(ord(char) < 32 or ord(char) == 127 for char in user_id):
        raise HTTPException(status_code=401, detail="missing or invalid X-Soha-User-ID")
    return {"id": user_id, "displayName": decode_header(request.headers.get("x-soha-user"))}


def connect():
    dsn = os.environ.get("DATABASE_URL", "").strip()
    if not dsn:
        raise HTTPException(status_code=503, detail="DATABASE_URL is not configured")
    try:
        return psycopg.connect(dsn, connect_timeout=5, row_factory=dict_row)
    except psycopg.Error as exc:
        raise HTTPException(status_code=503, detail="database unavailable") from exc


def entry_id(user_id: str, key: str) -> str:
    return hashlib.sha256(f"{user_id}\0{key}".encode()).hexdigest()


@app.get("/healthz")
def healthz():
    return {"status": "ok"}


@app.get("/")
def index():
    return {"service": "soha-app-hosting-python-starter", "endpoints": ["GET /healthz", "GET /api/entries", "POST /api/entries"]}


@app.get("/api/entries")
def list_entries(request: Request):
    user = read_identity(request)
    try:
        with connect() as connection:
            rows = connection.execute(
                "SELECT data FROM app.entries WHERE data->>'owner' = %s ORDER BY id LIMIT 200",
                (user["id"],),
            ).fetchall()
    except psycopg.Error as exc:
        raise HTTPException(status_code=503, detail="database unavailable") from exc
    return {"user": user, "entries": [row["data"] for row in rows]}


@app.post("/api/entries")
def save_entry(entry: EntryInput, request: Request):
    user = read_identity(request)
    data = {
        "key": entry.key,
        "value": entry.value,
        "owner": user["id"],
        "ownerDisplayName": user["displayName"],
        "updatedAt": datetime.now(timezone.utc).isoformat(),
    }
    try:
        with connect() as connection:
            row = connection.execute(
                "INSERT INTO app.entries (id, data) VALUES (%s, %s) "
                "ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data RETURNING data",
                (entry_id(user["id"], entry.key), Jsonb(data)),
            ).fetchone()
    except psycopg.Error as exc:
        raise HTTPException(status_code=503, detail="database unavailable") from exc
    return {"user": user, "entry": row["data"] if row else None}


if __name__ == "__main__":
    try:
        port = int(os.environ.get("PORT", "8080"))
    except ValueError:
        port = 8080
    uvicorn.run(app, host="0.0.0.0", port=port)
