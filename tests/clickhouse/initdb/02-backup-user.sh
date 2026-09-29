#!/bin/bash
# Creates the backup user with the same grants used in production
# (BACKUP + SHOW on everything, read/write on the S3 source for the destination).
set -e
clickhouse client -n <<SQL
CREATE USER IF NOT EXISTS ${CH_BACKUP_USER} IDENTIFIED WITH sha256_password BY '${CH_BACKUP_PASSWORD}';
GRANT BACKUP, SHOW ON *.* TO ${CH_BACKUP_USER};
GRANT READ, WRITE ON S3 TO ${CH_BACKUP_USER};
-- needed to poll the ASYNC backup status
GRANT SELECT ON system.backups TO ${CH_BACKUP_USER};
SQL
