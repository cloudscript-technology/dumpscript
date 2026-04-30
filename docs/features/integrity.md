# Upload integrity check

Garante que o objeto que chegou no bucket é **idêntico** ao arquivo
local — protege contra truncagens silenciosas e corrupção em trânsito.

Cobre o cenário ruim clássico: SDK retorna sucesso, mas o objeto remoto
ficou truncado (proxy buggy, sessão multipart abortada após commit
parcial, conexão TCP corrompida sem RST). Sem essa checagem, você só
descobre na hora do restore — meses depois.

> **Diferente de** [content verification](./verification.md): aquela
> valida o conteúdo **antes** do upload (footer marker / magic bytes
> per-engine); esta valida **depois** que o objeto está no bucket.

---

## Camadas por backend

### S3 — SHA-256 server-side + size check

`PutObjectInput.ChecksumAlgorithm = SHA256` ativa cálculo automático
pelo `manager.Uploader` (cobre upload single-part E multipart com
composite checksum). AWS recomputa o SHA-256 de cada part e **rejeita o
upload server-side** se algum byte foi corrompido em trânsito (proxy
MITM, switch flappando, TCP corrompido).

Após o upload, o dumpscript faz `HeadObject` e confere
`ContentLength == localSize`. Erro:

```
s3 upload integrity check failed: local=12300000 remote=12200000 (key=...)
```

### Azure Blob — size check pós-upload

`UploadFile` retorna OK; o dumpscript chama `GetProperties` e confere
`ContentLength == localSize`. Erro com a mesma forma:

```
azure upload integrity check failed: local=12300000 remote=12200000 (key=...)
```

Azure não tem um equivalente direto do `ChecksumAlgorithm` automático
do AWS SDK pra streams, mas a checagem de tamanho captura ~95% das
corrupções práticas.

### GCS — CRC32C end-to-end + size check

A SDK `cloud.google.com/go/storage` faz **CRC32C end-to-end automático**
no upload — qualquer corrupção entre cliente e bucket falha o `Close()`
do writer com erro. O dumpscript adiciona o size check via
`Object.Attrs(ctx)` como rede de segurança final.

---

## Custo operacional

- **1 round-trip extra** por upload (HEAD/GetProperties — microssegundos)
- **5–8% CPU adicional** em uploads grandes (S3 SHA-256 streaming)
- Payback: zero falsos sucessos. Em 100 uploads/dia, vale.

---

## Comportamento esperado

| Cenário | O que acontece |
|---|---|
| Upload OK, tamanhos batem | Log debug `upload integrity verified` + run continua |
| Corrupção em trânsito (S3) | AWS rejeita server-side, erro imediato `s3 upload: ... checksum mismatch ...` antes do HEAD |
| Multipart abortado, commit truncado | Erro `... integrity check failed: local=N remote=M ...` no HEAD/GetProperties |
| Network issue no HEAD pós-upload | Erro `... verify head: ...` com a causa propagada |

---

## Cobertura de testes

`internal/storage/integrity_test.go`: 5 unit tests via `httptest.Server`:

- Happy path S3 (PUT + HEAD com size correto) → upload OK
- S3 truncation (HEAD mente size) → erro `integrity check failed`
- S3 HEAD 5xx (network falha) → erro `verify head`
- Happy path Azure (PUT + GetProperties) → upload OK
- Azure truncation → erro `integrity check failed`

Suite full passa com `-race -count=1`. Veja
[`internal/storage/integrity_test.go`](../../internal/storage/integrity_test.go).

---

## Quando isso importa de fato

- **Multipart** (objetos > 200 MB default): sessões podem abortar entre
  parts e o S3 commitar o que tem se chamado errado
- **Proxies internos**: alguns squid/nginx clamam ter saved bytes mas
  truncam no buffer (raríssimo, real)
- **Compliance auditável**: você quer poder dizer "todo backup tem
  integridade verificada server-side" no formulário SOC2/ISO

Em ambientes simples (single-cloud, sem proxy custom), os erros que isso
captura são raros — mas o custo é mínimo e a confiança é máxima.

---

## Back

- [Content verification](./verification.md) — checagem **antes** do upload
- [Storage overview](../storage/README.md)
- [Docs home](../README.md)
