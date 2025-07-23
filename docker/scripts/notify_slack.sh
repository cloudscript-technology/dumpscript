#!/bin/sh
set -e

# Script para enviar notificações para o Slack via webhook
# Requer as seguintes variáveis de ambiente:
# SLACK_WEBHOOK_URL - URL do webhook do Slack
# SLACK_CHANNEL (opcional) - Canal específico para enviar a mensagem
# SLACK_USERNAME (opcional) - Nome do usuário que aparecerá como remetente

# Função para enviar notificação de falha
send_failure_notification() {
    local error_message="$1"
    local context="$2"
    local timestamp=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
    
    if [ -z "$SLACK_WEBHOOK_URL" ]; then
        echo "Warning: SLACK_WEBHOOK_URL not configured. Skipping Slack notification."
        return 0
    fi
    
    # Preparar dados do contexto
    local env_info=""
    if [ -n "$DB_TYPE" ]; then env_info="${env_info}Database Type: $DB_TYPE\n"; fi
    if [ -n "$DB_HOST" ]; then env_info="${env_info}Database Host: $DB_HOST\n"; fi
    if [ -n "$DB_NAME" ]; then env_info="${env_info}Database Name: $DB_NAME\n"; fi
    if [ -n "$PERIODICITY" ]; then env_info="${env_info}Backup Frequency: $PERIODICITY\n"; fi
    if [ -n "$S3_BUCKET" ]; then env_info="${env_info}S3 Bucket: $S3_BUCKET\n"; fi
    
    # Construir payload JSON
    local payload=$(cat <<EOF
{
    "channel": "${SLACK_CHANNEL:-#alerts}",
    "username": "${SLACK_USERNAME:-DumpScript Bot}",
    "icon_emoji": ":warning:",
    "attachments": [
        {
            "color": "danger",
            "fallback": "Database Backup Failed: $error_message",
            "title": ":exclamation: Database Backup Failure",
            "fields": [
                {
                    "title": "Error",
                    "value": "$error_message",
                    "short": false
                },
                {
                    "title": "Context",
                    "value": "$context",
                    "short": false
                },
                {
                    "title": "Environment Details",
                    "value": "$env_info",
                    "short": false
                },
                {
                    "title": "Timestamp",
                    "value": "$timestamp",
                    "short": true
                },
                {
                    "title": "Hostname",
                    "value": "$(hostname)",
                    "short": true
                }
            ],
            "footer": "DumpScript Monitoring",
            "ts": $(date +%s)
        }
    ]
}
EOF
    )
    
    echo "Sending Slack notification..."
    
    # Enviar para o Slack
    if curl -s -X POST \
        -H 'Content-type: application/json' \
        --data "$payload" \
        "$SLACK_WEBHOOK_URL" > /dev/null; then
        echo "Slack notification sent successfully."
    else
        echo "Failed to send Slack notification."
        return 1
    fi
}

# Função para enviar notificação de sucesso (opcional)
send_success_notification() {
    local s3_path="$1"
    local dump_size="$2"
    local timestamp=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
    
    if [ -z "$SLACK_WEBHOOK_URL" ]; then
        echo "Warning: SLACK_WEBHOOK_URL not configured. Skipping Slack notification."
        return 0
    fi
    
    # Só enviar notificação de sucesso se SLACK_NOTIFY_SUCCESS estiver habilitado
    if [ "$SLACK_NOTIFY_SUCCESS" != "true" ]; then
        return 0
    fi
    
    # Preparar dados do contexto
    local env_info=""
    if [ -n "$DB_TYPE" ]; then env_info="${env_info}Database Type: $DB_TYPE\n"; fi
    if [ -n "$DB_HOST" ]; then env_info="${env_info}Database Host: $DB_HOST\n"; fi
    if [ -n "$DB_NAME" ]; then env_info="${env_info}Database Name: $DB_NAME\n"; fi
    if [ -n "$PERIODICITY" ]; then env_info="${env_info}Backup Frequency: $PERIODICITY\n"; fi
    
    # Construir payload JSON
    local payload=$(cat <<EOF
{
    "channel": "${SLACK_CHANNEL:-#alerts}",
    "username": "${SLACK_USERNAME:-DumpScript Bot}",
    "icon_emoji": ":white_check_mark:",
    "attachments": [
        {
            "color": "good",
            "fallback": "Database Backup Completed Successfully",
            "title": ":heavy_check_mark: Database Backup Completed",
            "fields": [
                {
                    "title": "S3 Location",
                    "value": "$s3_path",
                    "short": false
                },
                {
                    "title": "Backup Size",
                    "value": "$dump_size bytes",
                    "short": true
                },
                {
                    "title": "Environment Details",
                    "value": "$env_info",
                    "short": false
                },
                {
                    "title": "Timestamp",
                    "value": "$timestamp",
                    "short": true
                },
                {
                    "title": "Hostname",
                    "value": "$(hostname)",
                    "short": true
                }
            ],
            "footer": "DumpScript Monitoring",
            "ts": $(date +%s)
        }
    ]
}
EOF
    )
    
    echo "Sending Slack success notification..."
    
    # Enviar para o Slack
    if curl -s -X POST \
        -H 'Content-type: application/json' \
        --data "$payload" \
        "$SLACK_WEBHOOK_URL" > /dev/null; then
        echo "Slack success notification sent successfully."
    else
        echo "Failed to send Slack success notification."
        return 1
    fi
}

# Permitir que o script seja usado como função ou executado diretamente
case "$1" in
    "failure")
        send_failure_notification "$2" "$3"
        ;;
    "success")
        send_success_notification "$2" "$3"
        ;;
    *)
        echo "Usage: $0 {failure|success} <message> [context]"
        echo "  failure: Send failure notification with error message and context"
        echo "  success: Send success notification with S3 path and dump size"
        exit 1
        ;;
esac 