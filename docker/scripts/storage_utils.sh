#!/bin/bash

# Storage Utilities - Unified storage abstraction using rclone
# Supports S3-compatible storage (AWS, MinIO) and Azure Blob Storage
#
# Required environment variables:
#   STORAGE_BACKEND - "s3" (default) or "azure"
#
# S3 backend variables:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY - AWS credentials
#   AWS_REGION - AWS region
#   S3_BUCKET - S3 bucket name
#   S3_PREFIX - S3 key prefix
#   AWS_S3_ENDPOINT_URL (optional) - Custom S3 endpoint for MinIO/compatible
#   AWS_SESSION_TOKEN (optional) - Temporary session token
#
# Azure backend variables:
#   AZURE_STORAGE_ACCOUNT - Storage account name
#   AZURE_STORAGE_KEY - Storage account access key (or use SAS token)
#   AZURE_STORAGE_SAS_TOKEN (optional) - SAS token (alternative to key)
#   AZURE_STORAGE_CONTAINER - Blob container name
#   AZURE_STORAGE_PREFIX - Blob key prefix
#
# Optional tuning variables:
#   STORAGE_UPLOAD_CUTOFF - Threshold for multipart upload (default: "200M")
#   STORAGE_CHUNK_SIZE - Chunk size for multipart upload (default: "100M")
#   STORAGE_UPLOAD_CONCURRENCY - Parallel upload threads (default: "4")

# Configuration
STORAGE_MAX_RETRIES=3
STORAGE_INITIAL_BACKOFF=5
STORAGE_MAX_BACKOFF=300
STORAGE_CREDENTIAL_REFRESH_INTERVAL=2700  # 45 minutes

# Tuning defaults
STORAGE_UPLOAD_CUTOFF="${STORAGE_UPLOAD_CUTOFF:-200M}"
STORAGE_CHUNK_SIZE="${STORAGE_CHUNK_SIZE:-100M}"
STORAGE_UPLOAD_CONCURRENCY="${STORAGE_UPLOAD_CONCURRENCY:-4}"

# Logging
storage_log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [storage] $1" >&2
}

# Exponential backoff calculation
storage_calculate_backoff() {
    local attempt=$1
    local backoff=$((STORAGE_INITIAL_BACKOFF * (2 ** (attempt - 1))))
    if [ $backoff -gt $STORAGE_MAX_BACKOFF ]; then
        backoff=$STORAGE_MAX_BACKOFF
    fi
    echo $backoff
}

# Get the effective storage backend
storage_get_backend() {
    echo "${STORAGE_BACKEND:-s3}"
}

# Get the bucket/container name
storage_get_container() {
    case "$(storage_get_backend)" in
        s3)    echo "$S3_BUCKET" ;;
        azure) echo "$AZURE_STORAGE_CONTAINER" ;;
    esac
}

# Get the prefix
storage_get_prefix() {
    case "$(storage_get_backend)" in
        s3)    echo "$S3_PREFIX" ;;
        azure) echo "${AZURE_STORAGE_PREFIX:-$S3_PREFIX}" ;;
    esac
}

# Build rclone flags for the configured backend
_storage_rclone_flags() {
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)
            local provider="AWS"
            local endpoint_flag=""

            if [ -n "$AWS_S3_ENDPOINT_URL" ]; then
                # Detectar GCS pelo endpoint
                if echo "$AWS_S3_ENDPOINT_URL" | grep -q "googleapis.com"; then
                    # GCS via S3-compat (HMAC): provider=GCS respeita --s3-endpoint,
                    # usa path-style por padrão e assinatura v4. --s3-no-check-bucket
                    # evita CreateBucket (bucket já existe, namespace global do GCS).
                    provider="GCS"
                    endpoint_flag="--s3-endpoint=$AWS_S3_ENDPOINT_URL --s3-no-check-bucket"
                else
                    provider="Other"
                    endpoint_flag="--s3-endpoint=$AWS_S3_ENDPOINT_URL --s3-force-path-style=true"
                fi
            fi

            echo "--s3-provider=$provider"
            echo "--s3-region=${AWS_REGION:-us-east-1}"
            echo "--s3-access-key-id=${AWS_ACCESS_KEY_ID:-}"
            echo "--s3-secret-access-key=${AWS_SECRET_ACCESS_KEY:-}"
            if [ -n "$AWS_SESSION_TOKEN" ]; then
                echo "--s3-session-token=$AWS_SESSION_TOKEN"
            fi
            if [ -n "$endpoint_flag" ]; then
                echo "$endpoint_flag"
            fi
            ;;
        azure)
            echo "--azureblob-account=${AZURE_STORAGE_ACCOUNT}"
            if [ -n "$AZURE_STORAGE_SAS_TOKEN" ]; then
                echo "--azureblob-sas-url=${AZURE_STORAGE_SAS_TOKEN}"
            elif [ -n "$AZURE_STORAGE_KEY" ]; then
                echo "--azureblob-key=${AZURE_STORAGE_KEY}"
            fi
            ;;
        *)
            storage_log "ERROR: Unknown storage backend: $backend"
            return 1
            ;;
    esac
}

# Build rclone upload tuning flags for the configured backend
_storage_upload_flags() {
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)
            echo "--s3-upload-cutoff=$STORAGE_UPLOAD_CUTOFF"
            echo "--s3-chunk-size=$STORAGE_CHUNK_SIZE"
            echo "--s3-upload-concurrency=$STORAGE_UPLOAD_CONCURRENCY"
            echo "--s3-storage-class=STANDARD_IA"
            ;;
        azure)
            echo "--azureblob-upload-cutoff=$STORAGE_UPLOAD_CUTOFF"
            echo "--azureblob-chunk-size=$STORAGE_CHUNK_SIZE"
            echo "--azureblob-upload-concurrency=$STORAGE_UPLOAD_CONCURRENCY"
            ;;
    esac
}

# Build the rclone remote path for a given relative path
_storage_remote_path() {
    local relative_path="$1"
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)    echo ":s3:${S3_BUCKET}/${relative_path}" ;;
        azure) echo ":azureblob:${AZURE_STORAGE_CONTAINER}/${relative_path}" ;;
    esac
}

# Build the rclone remote root (bucket/container only, for list operations)
_storage_remote_root() {
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)    echo ":s3:${S3_BUCKET}" ;;
        azure) echo ":azureblob:${AZURE_STORAGE_CONTAINER}" ;;
    esac
}

# Validate that the required environment variables are set for the configured backend
storage_validate_config() {
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)
            if [ -z "$S3_BUCKET" ]; then
                storage_log "ERROR: S3_BUCKET is required for s3 backend"
                return 1
            fi
            ;;
        azure)
            if [ -z "$AZURE_STORAGE_ACCOUNT" ]; then
                storage_log "ERROR: AZURE_STORAGE_ACCOUNT is required for azure backend"
                return 1
            fi
            if [ -z "$AZURE_STORAGE_KEY" ] && [ -z "$AZURE_STORAGE_SAS_TOKEN" ]; then
                storage_log "ERROR: AZURE_STORAGE_KEY or AZURE_STORAGE_SAS_TOKEN is required for azure backend"
                return 1
            fi
            if [ -z "$AZURE_STORAGE_CONTAINER" ]; then
                storage_log "ERROR: AZURE_STORAGE_CONTAINER is required for azure backend"
                return 1
            fi
            ;;
        *)
            storage_log "ERROR: Unknown STORAGE_BACKEND: $backend (must be 's3' or 'azure')"
            return 1
            ;;
    esac

    storage_log "Storage configuration validated (backend: $backend)"
    return 0
}

# Validate that a local file exists and is not empty
storage_validate_file() {
    local file_path="$1"

    if [ ! -f "$file_path" ]; then
        storage_log "ERROR: File not found: $file_path"
        return 1
    fi

    if [ ! -s "$file_path" ]; then
        storage_log "ERROR: File is empty: $file_path"
        return 1
    fi

    local file_size=$(stat -c%s "$file_path")
    storage_log "File validated: $file_path (size: $file_size bytes)"
    return 0
}

# Check if AWS credentials need refresh (for IRSA/STS)
_storage_check_credential_expiry() {
    if [ "$(storage_get_backend)" != "s3" ]; then
        return 1  # No refresh needed for non-S3 backends
    fi

    if [ -n "$AWS_SESSION_TOKEN" ]; then
        local current_time=$(date +%s)
        local last_refresh_time=${STORAGE_LAST_CREDENTIAL_REFRESH:-0}
        local time_since_refresh=$((current_time - last_refresh_time))

        if [ $time_since_refresh -gt $STORAGE_CREDENTIAL_REFRESH_INTERVAL ]; then
            storage_log "Credentials close to expiration, refreshing..."
            return 0  # Needs refresh
        fi
    fi
    return 1  # No refresh needed
}

# Refresh AWS credentials if needed
_storage_refresh_credentials() {
    local refresh_function="${1:-assume_aws_role}"

    if [ "$(storage_get_backend)" != "s3" ]; then
        return 0  # No refresh needed for non-S3 backends
    fi

    if command -v "$refresh_function" >/dev/null 2>&1; then
        storage_log "Refreshing credentials using: $refresh_function"
        if "$refresh_function"; then
            export STORAGE_LAST_CREDENTIAL_REFRESH=$(date +%s)
            storage_log "Credentials refreshed successfully"
            return 0
        else
            storage_log "WARNING: Failed to refresh credentials"
            return 1
        fi
    else
        storage_log "WARNING: Refresh function not available: $refresh_function"
        return 1
    fi
}

# Upload a local file to remote storage
# Usage: storage_upload <local_file> <remote_relative_path> [credential_refresh_function]
storage_upload() {
    local file_path="$1"
    local remote_path="$2"
    local refresh_function="${3:-}"
    local attempt=1

    if ! storage_validate_file "$file_path"; then
        return 1
    fi

    local remote=$(_storage_remote_path "$remote_path")
    # rclone copyto needs destination as a file path, so we use the directory + filename approach
    # Actually rclone copyto copies file to exact remote path
    local rclone_flags=$(_storage_rclone_flags)
    local upload_flags=$(_storage_upload_flags)
    local file_size=$(stat -c%s "$file_path")
    local backend=$(storage_get_backend)

    storage_log "Starting upload to $backend storage"
    storage_log "File: $file_path ($file_size bytes)"
    storage_log "Destination: $remote"

    # Initialize credential refresh timestamp
    export STORAGE_LAST_CREDENTIAL_REFRESH=$(date +%s)

    # For very large files (>10GB), start background credential refresh
    local refresh_pid=""
    if [ "$file_size" -gt 10000000000 ] && [ -n "$refresh_function" ] && [ "$backend" = "s3" ]; then
        storage_log "Very large file detected, starting background credential refresh"
        (
            while true; do
                sleep 2400  # Refresh every 40 minutes
                storage_log "Background credential refresh triggered"
                _storage_refresh_credentials "$refresh_function" || true
            done
        ) &
        refresh_pid=$!
        storage_log "Background credential refresh started (PID: $refresh_pid)"
    fi

    while [ $attempt -le $STORAGE_MAX_RETRIES ]; do
        storage_log "Upload attempt $attempt of $STORAGE_MAX_RETRIES"

        # Check if we need credential refresh
        if [ -n "$refresh_function" ]; then
            if _storage_check_credential_expiry; then
                _storage_refresh_credentials "$refresh_function" || true
                # Rebuild flags after credential refresh
                rclone_flags=$(_storage_rclone_flags)
            fi
        fi

        # Execute rclone upload
        # shellcheck disable=SC2086
        if rclone copyto "$file_path" "$remote" \
            --config="" \
            --no-check-dest \
            --log-level=INFO \
            $rclone_flags \
            $upload_flags; then
            storage_log "Upload completed successfully: $remote"
            # Stop background refresh
            if [ -n "$refresh_pid" ]; then
                kill $refresh_pid 2>/dev/null || true
            fi
            return 0
        else
            storage_log "Upload failed (attempt $attempt)"
        fi

        if [ $attempt -lt $STORAGE_MAX_RETRIES ]; then
            local backoff=$(storage_calculate_backoff $attempt)
            storage_log "Waiting $backoff seconds before next attempt..."
            sleep $backoff

            # Try credential refresh before next attempt
            if [ -n "$refresh_function" ]; then
                _storage_refresh_credentials "$refresh_function" || true
                rclone_flags=$(_storage_rclone_flags)
            fi
        fi

        attempt=$((attempt + 1))
    done

    # Stop background refresh on failure
    if [ -n "$refresh_pid" ]; then
        kill $refresh_pid 2>/dev/null || true
    fi

    storage_log "ERROR: Upload failed after $STORAGE_MAX_RETRIES attempts"
    return 1
}

# Download a file from remote storage
# Usage: storage_download <remote_relative_path> <local_file>
storage_download() {
    local remote_path="$1"
    local local_file="$2"

    local remote=$(_storage_remote_path "$remote_path")
    local rclone_flags=$(_storage_rclone_flags)
    local backend=$(storage_get_backend)

    storage_log "Downloading from $backend storage"
    storage_log "Source: $remote"
    storage_log "Destination: $local_file"

    # shellcheck disable=SC2086
    if rclone copyto "$remote" "$local_file" \
        --config="" \
        --log-level=INFO \
        $rclone_flags; then
        storage_log "Download completed successfully: $local_file"
        return 0
    else
        storage_log "ERROR: Download failed"
        return 1
    fi
}

# List objects in remote storage (outputs JSON lines via rclone lsjson)
# Usage: storage_list <remote_relative_prefix>
# Output format (one per line): DATE SIZE PATH
#   DATE: YYYY-MM-DD HH:MM:SS
#   SIZE: size in bytes
#   PATH: full key/path relative to bucket/container
storage_list() {
    local prefix="$1"

    local remote_root=$(_storage_remote_root)
    local rclone_flags=$(_storage_rclone_flags)
    local backend=$(storage_get_backend)

    storage_log "Listing objects in $backend storage at: $prefix"

    # Use rclone lsjson for structured output, then normalize with jq
    # shellcheck disable=SC2086
    rclone lsjson "$remote_root/$prefix" \
        --config="" \
        --recursive \
        --log-level=ERROR \
        $rclone_flags \
    | jq -r '.[] | select(.IsDir == false) | "\(.ModTime | split(".")[0] | gsub("T"; " "))  \(.Size)  '"$prefix"'\(.Path)"'

    return ${PIPESTATUS[0]:-$?}
}

# Delete a file from remote storage
# Usage: storage_delete <remote_relative_path>
storage_delete() {
    local remote_path="$1"

    local remote=$(_storage_remote_path "$remote_path")
    local rclone_flags=$(_storage_rclone_flags)
    local backend=$(storage_get_backend)

    storage_log "Deleting from $backend storage: $remote"

    # shellcheck disable=SC2086
    if rclone deletefile "$remote" \
        --config="" \
        --log-level=INFO \
        $rclone_flags; then
        storage_log "Delete completed successfully: $remote"
        return 0
    else
        storage_log "ERROR: Delete failed: $remote"
        return 1
    fi
}

# Get a human-readable display path for logging
# Usage: storage_display_path <relative_path>
storage_display_path() {
    local relative_path="$1"
    local backend=$(storage_get_backend)

    case "$backend" in
        s3)    echo "s3://${S3_BUCKET}/${relative_path}" ;;
        azure) echo "azure://${AZURE_STORAGE_CONTAINER}/${relative_path}" ;;
    esac
}
