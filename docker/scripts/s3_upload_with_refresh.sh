#!/bin/bash
set -e
set -o pipefail

# Backward compatibility wrapper for s3_upload_with_refresh.sh
# This script now delegates to storage_utils.sh which uses rclone
# for unified S3 and Azure Blob Storage support.
#
# Parameters:
# $1 - local file for upload
# $2 - S3 destination path (s3://bucket/path/file)
# $3 - credential refresh function (optional)

# Source storage utilities
if [ -f "/usr/local/bin/storage_utils.sh" ]; then
    . /usr/local/bin/storage_utils.sh
elif [ -f "$(dirname "$0")/storage_utils.sh" ]; then
    . "$(dirname "$0")/storage_utils.sh"
else
    echo "Error: Storage utilities not found."
    exit 1
fi

# Source AWS utilities if available
if [ -f "/usr/local/bin/aws_role_utils.sh" ]; then
    . /usr/local/bin/aws_role_utils.sh
elif [ -f "$(dirname "$0")/aws_role_utils.sh" ]; then
    . "$(dirname "$0")/aws_role_utils.sh"
fi

main() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"

    if [ -z "$file_path" ] || [ -z "$s3_path" ]; then
        echo "Usage: $0 <local_file> <s3_path> [refresh_function]"
        echo "Example: $0 /tmp/dump.sql.gz s3://bucket/path/dump.sql.gz assume_aws_role"
        exit 1
    fi

    # Parse s3://bucket/key from the s3_path
    # Remove "s3://" prefix
    local path_without_scheme="${s3_path#s3://}"
    # Extract bucket (first path component)
    local bucket="${path_without_scheme%%/*}"
    # Extract key (everything after bucket/)
    local key="${path_without_scheme#*/}"

    # Ensure S3_BUCKET is set for storage_utils
    export S3_BUCKET="${bucket}"
    export STORAGE_BACKEND="s3"

    storage_upload "$file_path" "$key" "$refresh_function"
}

# Execute main function if script is called directly
if [ "${0##*/}" = "s3_upload_with_refresh.sh" ]; then
    main "$@"
fi
