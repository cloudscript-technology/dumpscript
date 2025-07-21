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
CUTOFF_DATE=$(date -u +"%Y-%m-%d" -d "@$(( $(date +%s) - (${RETENTION_DAYS}*24*60*60) ))")

# Debug logs
echo "[DEBUG] S3_BUCKET: $S3_BUCKET"
echo "[DEBUG] S3_PREFIX: $S3_PREFIX"
echo "[DEBUG] PERIODICITY: $PERIODICITY"
echo "[DEBUG] RETENTION_DAYS: $RETENTION_DAYS"
echo "[DEBUG] BACKUP_PATH: $BACKUP_PATH"
echo "[DEBUG] CUTOFF_DATE: $CUTOFF_DATE"

# List, filter, and remove old backups
aws s3 ls "s3://${S3_BUCKET}/${BACKUP_PATH}" --recursive | while read -r line; do
    file_path=$(echo "$line" | awk '{print $4}')
    [ -z "$file_path" ] && continue
    # Extract backup date from path: .../YYYY/MM/DD/filename
    backup_date=$(echo "$file_path" | awk -F'/' '{print $(NF-4) "-" $(NF-3) "-" $(NF-2)}')
    echo "[DEBUG] Checking file: $file_path (backup date: $backup_date) - $CUTOFF_DATE"
    # Compare dates
    if [[ "$backup_date" < "$CUTOFF_DATE" ]]; then
        echo "[DEBUG] $backup_date < $CUTOFF_DATE: will remove"
        echo "Removing s3://${S3_BUCKET}/${BACKUP_PATH}${file_path} (backup date: $backup_date)"
        aws s3 rm "s3://${S3_BUCKET}/${BACKUP_PATH}${file_path}"
    else
        echo "[DEBUG] $backup_date >= $CUTOFF_DATE: keeping"
    fi
done 