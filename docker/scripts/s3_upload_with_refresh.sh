#!/bin/sh
set -e
set -o pipefail

# Script for S3 upload with automatic credential refresh
# Parameters:
# $1 - local file for upload
# $2 - S3 destination path
# $3 - credential refresh function (optional)

# Configuration
MAX_RETRIES=3
INITIAL_BACKOFF=5
MAX_BACKOFF=300
CREDENTIAL_REFRESH_INTERVAL=2700  # 45 minutos (tokens AWS duram 1 hora, refresh mais cedo)
MULTIPART_THRESHOLD=1000000000    # 1GB - better for very large files
MULTIPART_CHUNKSIZE=100000000     # 100MB por parte

# Function for logging with timestamp
log_with_timestamp() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Function to calculate exponential backoff
calculate_backoff() {
    local attempt=$1
    local backoff=$((INITIAL_BACKOFF * (2 ** (attempt - 1))))
    if [ $backoff -gt $MAX_BACKOFF ]; then
        backoff=$MAX_BACKOFF
    fi
    echo $backoff
}

# Function to check if credentials are close to expiration
check_credential_expiry() {
    if [ -n "$AWS_SESSION_TOKEN" ]; then
        # For temporary tokens, we check if they are close to expiration
        # Since we don't have direct access to expiration time, we use a fixed interval
        local current_time=$(date +%s)
        local last_refresh_time=${LAST_CREDENTIAL_REFRESH:-0}
        local time_since_refresh=$((current_time - last_refresh_time))
        
        if [ $time_since_refresh -gt $CREDENTIAL_REFRESH_INTERVAL ]; then
            log_with_timestamp "Credentials close to expiration, refreshing..."
            return 0  # Needs refresh
        fi
    fi
    return 1  # No refresh needed
}

# Source AWS utilities if available
if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
    . /usr/local/bin/aws_role_utils.sh
elif [ -f "$(dirname "$0")/aws_role_utils.sh" ]; then
    . "$(dirname "$0")/aws_role_utils.sh"
fi

# Function for credential refresh
refresh_credentials() {
    local refresh_function="$1"
    
    if [ -n "$refresh_function" ]; then
        # Handle specific known functions
        case "$refresh_function" in
            "assume_aws_role")
                if command -v assume_aws_role >/dev/null 2>&1; then
                    log_with_timestamp "Executing credential refresh using: $refresh_function"
                    if assume_aws_role; then
                        export LAST_CREDENTIAL_REFRESH=$(date +%s)
                        log_with_timestamp "Credentials refreshed successfully"
                        return 0
                    else
                        log_with_timestamp "Error refreshing credentials"
                        return 1
                    fi
                else
                    log_with_timestamp "Function assume_aws_role not available - AWS utilities not loaded"
                    return 1
                fi
                ;;
            *)
                # Try to call the function if it exists
                if command -v "$refresh_function" >/dev/null 2>&1; then
                    log_with_timestamp "Executing credential refresh using: $refresh_function"
                    if "$refresh_function"; then
                        export LAST_CREDENTIAL_REFRESH=$(date +%s)
                        log_with_timestamp "Credentials refreshed successfully"
                        return 0
                    else
                        log_with_timestamp "Error refreshing credentials"
                        return 1
                    fi
                else
                    log_with_timestamp "Refresh function not available or not found: $refresh_function"
                    return 1
                fi
                ;;
        esac
    else
        log_with_timestamp "No refresh function specified"
        return 1
    fi
}

# Function to check if file exists and get information
validate_file() {
    local file_path="$1"
    
    if [ ! -f "$file_path" ]; then
        log_with_timestamp "Error: File not found: $file_path"
        return 1
    fi
    
    if [ ! -s "$file_path" ]; then
        log_with_timestamp "Error: File is empty: $file_path"
        return 1
    fi
    
    local file_size=$(stat -c%s "$file_path")
    log_with_timestamp "File validated: $file_path (size: $file_size bytes)"
    return 0
}

# Function for simple upload (small files)
simple_upload() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    local attempt=1
    
    while [ $attempt -le $MAX_RETRIES ]; do
        log_with_timestamp "Attempt $attempt of $MAX_RETRIES for simple upload"
        
        # Verificar se precisa refresh de credenciais
        if check_credential_expiry; then
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Failed to refresh credentials"
        fi
        
        # Tentar upload
        if aws s3 cp "$file_path" "$s3_path" --no-progress; then
            log_with_timestamp "Simple upload completed successfully: $s3_path"
            return 0
        else
            log_with_timestamp "Simple upload failed (attempt $attempt)"
            
            if [ $attempt -lt $MAX_RETRIES ]; then
                local backoff=$(calculate_backoff $attempt)
                log_with_timestamp "Waiting $backoff seconds before next attempt..."
                sleep $backoff
                
                # Try credential refresh before next attempt
                refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Failed to refresh credentials"
            fi
        fi
        
        attempt=$((attempt + 1))
    done
    
    log_with_timestamp "Error: Simple upload failed after $MAX_RETRIES attempts"
    return 1
}

# Function for multipart upload with automatic credential refresh
multipart_upload_with_refresh() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    
    log_with_timestamp "Starting multipart upload with automatic credential refresh"
    
    # Configurar multipart upload
    aws configure set default.s3.multipart_threshold $MULTIPART_THRESHOLD
    aws configure set default.s3.multipart_chunksize $MULTIPART_CHUNKSIZE
    aws configure set default.s3.max_concurrent_requests 2  # Reduce for very large files
    
    # Start background credential refresh
    local refresh_pid=""
    if [ -n "$refresh_function" ]; then
        (
            while true; do
                sleep 2400  # Refresh every 40 minutes
                log_with_timestamp "Background credential refresh started"
                if refresh_credentials "$refresh_function"; then
                    log_with_timestamp "Background credential refresh completed successfully"
                else
                    log_with_timestamp "Warning: Background credential refresh failed"
                fi
            done
        ) &
        refresh_pid=$!
        log_with_timestamp "Background credential refresh started (PID: $refresh_pid)"
    fi
    
    # Execute upload with extended timeout
    local upload_result=0
    if ! timeout 21600 aws s3 cp "$file_path" "$s3_path" --storage-class STANDARD_IA; then
        upload_result=1
        log_with_timestamp "Multipart upload failed"
    else
        log_with_timestamp "Multipart upload completed successfully: $s3_path"
    fi
    
    # Stop background refresh process
    if [ -n "$refresh_pid" ]; then
        kill $refresh_pid 2>/dev/null || true
        log_with_timestamp "Background credential refresh stopped"
    fi
    
    return $upload_result
}

# Function for multipart upload (large files) - version with fallback
multipart_upload() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    local attempt=1
    
    # For very large files (>10GB), use version with automatic refresh
    local file_size=$(stat -c%s "$file_path")
    if [ $file_size -gt 10000000000 ]; then
        log_with_timestamp "Very large file detected ($file_size bytes), using multipart upload with automatic refresh"
        return multipart_upload_with_refresh "$file_path" "$s3_path" "$refresh_function"
    fi
    
    while [ $attempt -le $MAX_RETRIES ]; do
        log_with_timestamp "Attempt $attempt of $MAX_RETRIES for multipart upload"
        
        # Verificar se precisa refresh de credenciais
        if check_credential_expiry; then
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Failed to refresh credentials"
        fi
        
        # Configure multipart upload with progress monitoring
        if aws configure set default.s3.multipart_threshold $MULTIPART_THRESHOLD && \
           aws configure set default.s3.multipart_chunksize $MULTIPART_CHUNKSIZE && \
           aws configure set default.s3.max_concurrent_requests 3; then
            
            # Try upload with callback for credential refresh
            # For very large uploads, use longer timeout and more frequent refresh
            if timeout 14400 aws s3 cp "$file_path" "$s3_path" --storage-class STANDARD_IA; then
                log_with_timestamp "Multipart upload completed successfully: $s3_path"
                return 0
            else
                log_with_timestamp "Multipart upload failed (attempt $attempt)"
            fi
        else
            log_with_timestamp "Error configuring multipart upload parameters"
        fi
        
        if [ $attempt -lt $MAX_RETRIES ]; then
            local backoff=$(calculate_backoff $attempt)
            log_with_timestamp "Waiting $backoff seconds before next attempt..."
            sleep $backoff
            
            # Try credential refresh before next attempt
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Failed to refresh credentials"
        fi
        
        attempt=$((attempt + 1))
    done
    
    log_with_timestamp "Error: Multipart upload failed after $MAX_RETRIES attempts"
    return 1
}

# Main function
main() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    
    if [ -z "$file_path" ] || [ -z "$s3_path" ]; then
        echo "Usage: $0 <local_file> <s3_path> [refresh_function]"
        echo "Exemplo: $0 /tmp/dump.sql.gz s3://bucket/path/dump.sql.gz assume_aws_role"
        exit 1
    fi
    
    log_with_timestamp "Starting S3 upload with automatic credential refresh"
    log_with_timestamp "File: $file_path"
    log_with_timestamp "Destino: $s3_path"
    log_with_timestamp "Refresh function: ${refresh_function:-'not specified'}"
    
    # Validate file
    if ! validate_file "$file_path"; then
        exit 1
    fi
    
    local file_size=$(stat -c%s "$file_path")
    
    # Initialize timestamp of last credential update
    export LAST_CREDENTIAL_REFRESH=$(date +%s)
    
    # Decide upload type based on file size
    if [ $file_size -gt $MULTIPART_THRESHOLD ]; then
        log_with_timestamp "Large file detected ($file_size bytes), using multipart upload"
        if ! multipart_upload "$file_path" "$s3_path" "$refresh_function"; then
            log_with_timestamp "Error: Multipart upload failed"
            exit 1
        fi
    else
        log_with_timestamp "Small file detected ($file_size bytes), using simple upload"
        if ! simple_upload "$file_path" "$s3_path" "$refresh_function"; then
            log_with_timestamp "Error: Simple upload failed"
            exit 1
        fi
    fi
}

# Execute main function if script is called directly
if [ "${0##*/}" = "s3_upload_with_refresh.sh" ]; then
    main "$@"
fi