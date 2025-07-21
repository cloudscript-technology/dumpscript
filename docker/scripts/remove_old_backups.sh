#!/bin/bash
set -e

# Required variables
: "${S3_PREFIX:?S3_PREFIX not defined}"
: "${PERIODICITY:?PERIODICITY not defined}"
: "${RETENTION_DAYS:?RETENTION_DAYS not defined}"

if [ "${RETENTION_DAYS}" -le 0 ]; then
  echo "RETENTION_DAYS is not greater than zero. Skipping old backup removal."
  exit 0
fi

# BACKUP_NAME is optional, can be part of the prefix

# List all objects in S3 for the periodicity
BACKUP_PATH="${S3_PREFIX}/${PERIODICITY}/"

# Cutoff date for retention
CUTOFF_DATE=$(date -u -r $(( $(date +%s) - (${RETENTION_DAYS}*24*60*60) )) +%Y-%m-%d)

# List, filter, and remove old backups
aws s3 ls "s3://${BACKUP_PATH}" --recursive | while read -r line; do
    # Example line: 2024-06-01 12:00:00   12345 backups/diario/2024/06/01/backup.sql.gz
    file_date=$(echo "$line" | awk '{print $1}')
    file_path=$(echo "$line" | awk '{print $4}')
    # If no file, skip
    [ -z "$file_path" ] && continue
    # Compare dates
    if [[ "$file_date" < "$CUTOFF_DATE" ]]; then
        echo "Removing s3://${BACKUP_PATH}${file_path} (date: $file_date)"
        aws s3 rm "s3://${BACKUP_PATH}${file_path}"
    fi
done 