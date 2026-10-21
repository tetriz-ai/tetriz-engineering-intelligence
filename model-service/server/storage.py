"""Postgres storage for classifications (the reference storage form).

Optional: the orchestrator persists each result here when DATABASE_URL is set,
otherwise it runs stateless. psycopg is imported lazily so the module (and the
orchestrator) import without a DB present — storage is only touched when enabled.
Runs against the Postgres container in docker-compose.yml.
"""

from __future__ import annotations

_SCHEMA = """
CREATE TABLE IF NOT EXISTS classifications (
    id            BIGSERIAL PRIMARY KEY,
    title         TEXT NOT NULL,
    branch        TEXT NOT NULL DEFAULT '',
    type_bucket   TEXT,
    type_method   TEXT NOT NULL,
    type_score    DOUBLE PRECISION,
    domain_bucket TEXT,
    domain_method TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
"""


class Storage:
    def __init__(self, dsn: str) -> None:
        import psycopg  # lazy — only when storage is enabled
        self._conn = psycopg.connect(dsn, autocommit=True)
        with self._conn.cursor() as cur:
            cur.execute(_SCHEMA)

    def save(self, *, title: str, branch: str, type_bucket, type_method: str,
             type_score, domain_bucket, domain_method: str) -> int:
        with self._conn.cursor() as cur:
            cur.execute(
                """INSERT INTO classifications
                   (title, branch, type_bucket, type_method, type_score, domain_bucket, domain_method)
                   VALUES (%s,%s,%s,%s,%s,%s,%s) RETURNING id""",
                (title, branch, type_bucket, type_method, type_score, domain_bucket, domain_method),
            )
            return cur.fetchone()[0]

    def close(self) -> None:
        self._conn.close()
