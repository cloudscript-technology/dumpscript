#!/bin/sh
set -e
set -o pipefail

# Script para upload S3 com refresh automático de credenciais
# Parâmetros:
# $1 - arquivo local para upload
# $2 - caminho S3 de destino
# $3 - função de refresh de credenciais (opcional)

# Configurações
MAX_RETRIES=3
INITIAL_BACKOFF=5
MAX_BACKOFF=300
CREDENTIAL_REFRESH_INTERVAL=3300  # 55 minutos (tokens AWS duram 1 hora)
MULTIPART_THRESHOLD=100000000     # 100MB
MULTIPART_CHUNKSIZE=50000000      # 50MB por parte

# Função para log com timestamp
log_with_timestamp() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Função para calcular backoff exponencial
calculate_backoff() {
    local attempt=$1
    local backoff=$((INITIAL_BACKOFF * (2 ** (attempt - 1))))
    if [ $backoff -gt $MAX_BACKOFF ]; then
        backoff=$MAX_BACKOFF
    fi
    echo $backoff
}

# Função para verificar se as credenciais estão próximas do vencimento
check_credential_expiry() {
    if [ -n "$AWS_SESSION_TOKEN" ]; then
        # Para tokens temporários, verificamos se estão próximos do vencimento
        # Como não temos acesso direto ao tempo de expiração, usamos um intervalo fixo
        local current_time=$(date +%s)
        local last_refresh_time=${LAST_CREDENTIAL_REFRESH:-0}
        local time_since_refresh=$((current_time - last_refresh_time))
        
        if [ $time_since_refresh -gt $CREDENTIAL_REFRESH_INTERVAL ]; then
            log_with_timestamp "Credenciais próximas do vencimento, refreshing..."
            return 0  # Precisa refresh
        fi
    fi
    return 1  # Não precisa refresh
}

# Função para refresh de credenciais
refresh_credentials() {
    local refresh_function="$1"
    
    if [ -n "$refresh_function" ] && command -v "$refresh_function" >/dev/null 2>&1; then
        log_with_timestamp "Executando refresh de credenciais usando: $refresh_function"
        if "$refresh_function"; then
            export LAST_CREDENTIAL_REFRESH=$(date +%s)
            log_with_timestamp "Credenciais refreshed com sucesso"
            return 0
        else
            log_with_timestamp "Erro ao fazer refresh das credenciais"
            return 1
        fi
    else
        log_with_timestamp "Função de refresh não disponível ou não encontrada: $refresh_function"
        return 1
    fi
}

# Função para verificar se o arquivo existe e obter informações
validate_file() {
    local file_path="$1"
    
    if [ ! -f "$file_path" ]; then
        log_with_timestamp "Erro: Arquivo não encontrado: $file_path"
        return 1
    fi
    
    if [ ! -s "$file_path" ]; then
        log_with_timestamp "Erro: Arquivo está vazio: $file_path"
        return 1
    fi
    
    local file_size=$(stat -c%s "$file_path")
    log_with_timestamp "Arquivo validado: $file_path (tamanho: $file_size bytes)"
    echo $file_size
}

# Função para upload simples (arquivos pequenos)
simple_upload() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    local attempt=1
    
    while [ $attempt -le $MAX_RETRIES ]; do
        log_with_timestamp "Tentativa $attempt de $MAX_RETRIES para upload simples"
        
        # Verificar se precisa refresh de credenciais
        if check_credential_expiry; then
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Falha no refresh de credenciais"
        fi
        
        # Tentar upload
        if aws s3 cp "$file_path" "$s3_path" --no-progress; then
            log_with_timestamp "Upload simples concluído com sucesso: $s3_path"
            return 0
        else
            log_with_timestamp "Falha no upload simples (tentativa $attempt)"
            
            if [ $attempt -lt $MAX_RETRIES ]; then
                local backoff=$(calculate_backoff $attempt)
                log_with_timestamp "Aguardando $backoff segundos antes da próxima tentativa..."
                sleep $backoff
                
                # Tentar refresh de credenciais antes da próxima tentativa
                refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Falha no refresh de credenciais"
            fi
        fi
        
        attempt=$((attempt + 1))
    done
    
    log_with_timestamp "Erro: Upload simples falhou após $MAX_RETRIES tentativas"
    return 1
}

# Função para upload multipart (arquivos grandes)
multipart_upload() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    local attempt=1
    
    while [ $attempt -le $MAX_RETRIES ]; do
        log_with_timestamp "Tentativa $attempt de $MAX_RETRIES para upload multipart"
        
        # Verificar se precisa refresh de credenciais
        if check_credential_expiry; then
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Falha no refresh de credenciais"
        fi
        
        # Configurar multipart upload com monitoramento de progresso
        if aws configure set default.s3.multipart_threshold $MULTIPART_THRESHOLD && \
           aws configure set default.s3.multipart_chunksize $MULTIPART_CHUNKSIZE && \
           aws configure set default.s3.max_concurrent_requests 3; then
            
            # Tentar upload com callback para refresh de credenciais
            if timeout 7200 aws s3 cp "$file_path" "$s3_path" --storage-class STANDARD_IA; then
                log_with_timestamp "Upload multipart concluído com sucesso: $s3_path"
                return 0
            else
                log_with_timestamp "Falha no upload multipart (tentativa $attempt)"
            fi
        else
            log_with_timestamp "Erro ao configurar parâmetros de multipart upload"
        fi
        
        if [ $attempt -lt $MAX_RETRIES ]; then
            local backoff=$(calculate_backoff $attempt)
            log_with_timestamp "Aguardando $backoff segundos antes da próxima tentativa..."
            sleep $backoff
            
            # Tentar refresh de credenciais antes da próxima tentativa
            refresh_credentials "$refresh_function" || log_with_timestamp "Warning: Falha no refresh de credenciais"
        fi
        
        attempt=$((attempt + 1))
    done
    
    log_with_timestamp "Erro: Upload multipart falhou após $MAX_RETRIES tentativas"
    return 1
}

# Função principal
main() {
    local file_path="$1"
    local s3_path="$2"
    local refresh_function="$3"
    
    if [ -z "$file_path" ] || [ -z "$s3_path" ]; then
        echo "Uso: $0 <arquivo_local> <caminho_s3> [função_refresh]"
        echo "Exemplo: $0 /tmp/dump.sql.gz s3://bucket/path/dump.sql.gz assume_aws_role"
        exit 1
    fi
    
    log_with_timestamp "Iniciando upload S3 com refresh automático de credenciais"
    log_with_timestamp "Arquivo: $file_path"
    log_with_timestamp "Destino: $s3_path"
    log_with_timestamp "Função de refresh: ${refresh_function:-'não especificada'}"
    
    # Validar arquivo
    local file_size
    if ! file_size=$(validate_file "$file_path"); then
        exit 1
    fi
    
    # Inicializar timestamp de última atualização de credenciais
    export LAST_CREDENTIAL_REFRESH=$(date +%s)
    
    # Decidir tipo de upload baseado no tamanho do arquivo
    if [ $file_size -gt $MULTIPART_THRESHOLD ]; then
        log_with_timestamp "Arquivo grande detectado ($file_size bytes), usando upload multipart"
        multipart_upload "$file_path" "$s3_path" "$refresh_function"
    else
        log_with_timestamp "Arquivo pequeno detectado ($file_size bytes), usando upload simples"
        simple_upload "$file_path" "$s3_path" "$refresh_function"
    fi
}

# Executar função principal se o script for chamado diretamente
if [ "${0##*/}" = "s3_upload_with_refresh.sh" ]; then
    main "$@"
fi