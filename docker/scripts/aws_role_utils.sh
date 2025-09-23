#!/bin/sh

# AWS Role Utilities - Shared functions for AWS role assumption
# This script contains reusable functions for AWS authentication

# Função para assumir role AWS (reutilizável)
assume_aws_role() {
    if [ -z "$AWS_ROLE_ARN" ]; then
        echo "No AWS_ROLE_ARN defined, skipping role assumption"
        return 0
    fi

    echo "Assuming AWS role: $AWS_ROLE_ARN"
    
    # Check if the service account token is available
    if [ -f "/var/run/secrets/eks.amazonaws.com/serviceaccount/token" ]; then
        echo "Service account token found"
        
        # Read the token from the file
        WEB_IDENTITY_TOKEN=$(cat /var/run/secrets/eks.amazonaws.com/serviceaccount/token)
        if [ -z "$WEB_IDENTITY_TOKEN" ]; then
            error_msg="Service account token is empty"
            echo "Error: $error_msg"
            # Only call notify_failure if the function exists (from parent script)
            if command -v notify_failure >/dev/null 2>&1; then
                notify_failure "$error_msg" "AWS authentication failed - IRSA token issue"
            fi
            return 1
        fi
        
        export AWS_ROLE_SESSION_NAME="dumpscript-$(date +%s)"
        
        # Assume the role using the service account token
        echo "Assuming role using IRSA..."
        echo "Role ARN: $AWS_ROLE_ARN"
        echo "Role Session Name: $AWS_ROLE_SESSION_NAME"
        
        TEMP_ROLE=$(aws sts assume-role-with-web-identity \
          --role-arn "$AWS_ROLE_ARN" \
          --role-session-name "$AWS_ROLE_SESSION_NAME" \
          --web-identity-token "$WEB_IDENTITY_TOKEN" \
          --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
          --output text)
        
        if [ $? -ne 0 ]; then
            echo "Error assuming role. Attempting to use default credentials."
            return 1
        else
            export AWS_ACCESS_KEY_ID=$(echo $TEMP_ROLE | cut -d' ' -f1)
            export AWS_SECRET_ACCESS_KEY=$(echo $TEMP_ROLE | cut -d' ' -f2)
            export AWS_SESSION_TOKEN=$(echo $TEMP_ROLE | cut -d' ' -f3)
            
            echo "Role assumed successfully!"
            echo "AWS_ACCESS_KEY_ID: ${AWS_ACCESS_KEY_ID:0:10}..."
            return 0
        fi
    else
        echo "Warning: Service account token not found at /var/run/secrets/eks.amazonaws.com/serviceaccount/token"
        echo "Listing available files:"
        find /var/run/secrets -name "*token*" -type f 2>/dev/null || echo "No token found"
        echo "Attempting to use default credentials."
        return 1
    fi
}