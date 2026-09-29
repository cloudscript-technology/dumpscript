-- Executed once by the official image on first start (as the admin user).
CREATE DATABASE IF NOT EXISTS demo;

CREATE TABLE IF NOT EXISTS demo.events
(
    event_date Date,
    event_id   UInt64,
    payload    String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_date, event_id);

INSERT INTO demo.events
SELECT toDate('2026-01-01') + intDiv(number, 1000), number, concat('payload-', toString(number))
FROM numbers(50000);

CREATE DATABASE IF NOT EXISTS demo_other;
CREATE TABLE IF NOT EXISTS demo_other.kv (k String, v String) ENGINE = MergeTree ORDER BY k;
INSERT INTO demo_other.kv VALUES ('a', '1'), ('b', '2');
