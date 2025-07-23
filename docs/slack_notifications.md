# Notificações do Slack para DumpScript

Este documento explica como configurar notificações do Slack para receber alertas quando ocorrem falhas no processo de backup do banco de dados.

## Configuração do Webhook do Slack

### 1. Criar um App do Slack

1. Acesse [https://api.slack.com/apps](https://api.slack.com/apps)
2. Clique em "Create New App"
3. Escolha "From scratch"
4. Dê um nome ao seu app (ex: "DumpScript Alerts")
5. Selecione o workspace do Slack

### 2. Configurar Incoming Webhooks

1. No painel do app, vá para "Incoming Webhooks"
2. Ative o toggle "Activate Incoming Webhooks"
3. Clique em "Add New Webhook to Workspace"
4. Selecione o canal onde deseja receber as notificações
5. Copie a URL do webhook gerada

### 3. Variáveis de Ambiente

Configure as seguintes variáveis de ambiente no seu deployment:

```bash
# Obrigatório - URL do webhook do Slack
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX

# Opcional - Canal específico (sobrescreve o canal configurado no webhook)
SLACK_CHANNEL=#database-alerts

# Opcional - Nome do usuário que aparece como remetente
SLACK_USERNAME=DumpScript Bot

# Opcional - Enviar notificações de sucesso também (padrão: false)
SLACK_NOTIFY_SUCCESS=true
```

## Tipos de Notificação

### Notificações de Falha (Automáticas)

As seguintes falhas irão gerar notificações automáticas:

- **Configuração inválida**: DB_TYPE ou PERIODICITY não especificados
- **Falha de autenticação AWS**: Problemas com tokens IRSA ou credenciais
- **Falha no dump do banco**: Problemas de conectividade ou credenciais do banco
- **Arquivo corrompido**: Dump gerado está vazio ou corrompido
- **Falha no upload S3**: Problemas com permissões ou conectividade AWS
- **Falha na instalação de clientes**: pg_dump ou mysqldump não instalados corretamente

Cada notificação inclui:
- Mensagem de erro detalhada
- Contexto da falha
- Detalhes do ambiente (tipo de banco, host, bucket S3, etc.)
- Timestamp e hostname
- Código de cores (vermelho para falhas)

### Notificações de Sucesso (Opcionais)

Se `SLACK_NOTIFY_SUCCESS=true`, você receberá notificações quando:
- O backup for concluído com sucesso
- O arquivo for enviado para o S3 corretamente

Essas notificações incluem:
- Localização do backup no S3
- Tamanho do arquivo de backup
- Detalhes do ambiente
- Timestamp e hostname
- Código de cores (verde para sucesso)

## Exemplo de Deployment com Slack

### Kubernetes ConfigMap/Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: slack-webhook
type: Opaque
stringData:
  webhook-url: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: slack-config
data:
  SLACK_CHANNEL: "#database-alerts"
  SLACK_USERNAME: "DumpScript Bot"
  SLACK_NOTIFY_SUCCESS: "false"
```

### Helm Values

```yaml
# values.yaml
slack:
  enabled: true
  webhookUrl: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
  channel: "#database-alerts"
  username: "DumpScript Bot"
  notifySuccess: false
```

### Docker Compose

```yaml
version: '3.8'
services:
  dumpscript:
    image: dumpscript:latest
    environment:
      - SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
      - SLACK_CHANNEL=#database-alerts
      - SLACK_USERNAME=DumpScript Bot
      - SLACK_NOTIFY_SUCCESS=false
      # ... outras variáveis
```

## Teste das Notificações

Para testar se as notificações estão funcionando, você pode:

1. **Executar o script diretamente**:
   ```bash
   docker exec -it <container> /usr/local/bin/notify_slack.sh failure "Teste de notificação" "Teste manual"
   ```

2. **Simular uma falha de configuração**:
   ```bash
   # Remover temporariamente DB_TYPE para gerar erro
   docker run --rm -e SLACK_WEBHOOK_URL=<sua-url> dumpscript:latest
   ```

## Troubleshooting

### Webhook não funciona
- Verifique se a URL do webhook está correta
- Confirme se o app do Slack tem permissões no canal
- Teste o webhook manualmente com curl:
  ```bash
  curl -X POST -H 'Content-type: application/json' \
    --data '{"text":"Teste"}' \
    YOUR_WEBHOOK_URL
  ```

### Notificações não aparecem
- Verifique se `SLACK_WEBHOOK_URL` está definido
- Confirme se o script `notify_slack.sh` existe em `/usr/local/bin/`
- Verifique se o script tem permissões de execução
- Olhe os logs do container para mensagens de erro

### Canal errado
- Verifique a variável `SLACK_CHANNEL`
- Confirme se o bot tem permissão para postar no canal especificado
- Use o ID do canal em vez do nome se necessário (ex: `C1234567890`)

## Formato das Mensagens

As mensagens seguem o formato de "attachments" do Slack para melhor visualização:

- **Cor**: Vermelho para falhas, verde para sucessos
- **Título**: Indica claramente o tipo de evento
- **Campos**: Organizados em seções (Error, Context, Environment Details, etc.)
- **Footer**: Identifica a origem como "DumpScript Monitoring"
- **Timestamp**: Incluído automaticamente

## Considerações de Segurança

- **Nunca commitar** URLs de webhook no código
- Use secrets/variáveis de ambiente para a configuração
- Considere rotacionar webhooks periodicamente
- Limite as permissões do app do Slack apenas aos canais necessários 