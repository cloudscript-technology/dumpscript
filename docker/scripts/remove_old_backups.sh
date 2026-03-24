#!/bin/bash
set -e
set -o pipefail

# Required variables
: "${PERIODICITY:?PERIODICITY not defined}"
: "${RETENTION_DAYS:?RETENTION_DAYS not defined}"

if [ "${RETENTION_DAYS}" -le 0 ]; then
  echo "RETENTION_DAYS is not greater than zero. Skipping old backup removal."
  exit 0
fi

# Source storage utilities
if [ -f "/usr/local/bin/storage_utils.sh" ]; then
    . /usr/local/bin/storage_utils.sh
elif [ -f "$(dirname "$0")/storage_utils.sh" ]; then
    . "$(dirname "$0")/storage_utils.sh"
else
    echo "Error: Storage utilities not found."
    exit 1
fi

# Validate storage configuration
if ! storage_validate_config; then
    echo "Warning: Storage configuration invalid. Skipping retention cleanup."
    exit 0
fi

# Source AWS utilities and assume role (S3 backend only)
if [ "$(storage_get_backend)" = "s3" ]; then
    if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
        . /usr/local/bin/aws_role_utils.sh
        assume_aws_role || true
    fi

    # Optional: validate identity for troubleshooting
    if command -v aws >/dev/null 2>&1; then
        AWS_EFFECTIVE_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}"
        aws sts get-caller-identity --region "$AWS_EFFECTIVE_REGION" >/dev/null 2>&1 || echo "[WARN] Unable to validate AWS identity (sts get-caller-identity)"
    fi
fi

# Determine prefix and backup path
STORAGE_PREFIX=$(storage_get_prefix)

# Validate prefix is set
if [ -z "$STORAGE_PREFIX" ]; then
    echo "Error: Storage prefix not defined (S3_PREFIX or AZURE_STORAGE_PREFIX)"
    exit 1
fi

BACKUP_PATH="${STORAGE_PREFIX}/${PERIODICITY}/"

# Cutoff date for retention
CUTOFF_DATE=$(date -u +"%Y-%m-%d" -d "@$(( $(date +%s) - (${RETENTION_DAYS}*24*60*60) ))")

# Debug logs
echo "[DEBUG] STORAGE_BACKEND: $(storage_get_backend)"
echo "[DEBUG] Container/Bucket: $(storage_get_container)"
echo "[DEBUG] Prefix: $STORAGE_PREFIX"
echo "[DEBUG] PERIODICITY: $PERIODICITY"
echo "[DEBUG] RETENTION_DAYS: $RETENTION_DAYS"
echo "[DEBUG] BACKUP_PATH: $BACKUP_PATH"
echo "[DEBUG] CUTOFF_DATE: $CUTOFF_DATE"

# List, filter, and remove old backups
TMP_LIST=$(mktemp)
TMP_ERR=$(mktemp)

if ! storage_list "${BACKUP_PATH}" >"$TMP_LIST" 2>"$TMP_ERR"; then
  DISPLAY_PATH=$(storage_display_path "$BACKUP_PATH")
  echo "Warning: Failed to list backups at $DISPLAY_PATH"
  echo "[DEBUG] List error output:"; head -100 "$TMP_ERR" 2>/dev/null || true
  rm -f "$TMP_LIST" "$TMP_ERR"
  echo "Skipping retention cleanup and continuing with dump."
  exit 0
fi

while read -r line; do
    # storage_list output format: "DATE  SIZE  PATH"
    file_path=$(echo "$line" | awk '{print $3}')
    [ -z "$file_path" ] && continue
    # Only process files ending with .sql.gz or .archive.gz
    if [[ ! "$file_path" =~ \.(sql|archive)(\.gz)?$ ]]; then
        echo "[DEBUG] Skipping non-backup entry: $file_path"
        continue
    fi
    # Extract backup date from path using regex
    if [[ "$file_path" =~ ([0-9]{4})/([0-9]{2})/([0-9]{2})/ ]]; then
        backup_date="${BASH_REMATCH[1]}-${BASH_REMATCH[2]}-${BASH_REMATCH[3]}"
    else
        echo "[DEBUG] Could not extract backup date from path: $file_path"
        continue
    fi
    echo "[DEBUG] Checking file: $file_path (backup date: $backup_date) - $CUTOFF_DATE"
    # Compare dates
    if [[ "$backup_date" < "$CUTOFF_DATE" ]]; then
        DISPLAY_FILE=$(storage_display_path "$file_path")
        echo "[DEBUG] $backup_date < $CUTOFF_DATE: will remove"
        echo "Removing $DISPLAY_FILE (backup date: $backup_date)"
        storage_delete "$file_path"
    else
        echo "[DEBUG] $backup_date >= $CUTOFF_DATE: keeping"
    fi
done < "$TMP_LIST"

rm -f "$TMP_LIST" "$TMP_ERR"
