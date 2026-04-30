# Adding a new engine

Cada engine vive isolado em `internal/dumper/<engine>.go` (e opcionalmente
`internal/restorer/<engine>.go`). O wiring é via `init() → Register()` —
**zero edits** em `factory.go` ou `cli.go`.

Este guia mostra o passo a passo usando um engine fictício `fooDB`.

---

## Anatomia de um engine

Cada engine implementa duas interfaces (uma é opcional):

```go
// Obrigatório — em internal/dumper/dumper.go
type Dumper interface {
    Dump(ctx context.Context, w io.Writer) error
    Name() string  // identificador estável (lowercase, sem espaços)
}

// Opcional — em internal/restorer/restorer.go
type Restorer interface {
    Restore(ctx context.Context, r io.Reader) error
    Name() string
}
```

Engines append-only (ex: Redis RDB) podem **omitir** a Restorer — basta
não registrar. O CR `Restore` retornará `ErrEngineRestoreUnsupported`.

---

## Passo 1: criar o dumper

`internal/dumper/foodb.go`:

```go
package dumper

import (
    "context"
    "io"
    "log/slog"
    "os/exec"

    "github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
    Register("foodb", func(cfg *config.Config, log *slog.Logger) (Dumper, bool) {
        if cfg.DBType != "foodb" {
            return nil, false
        }
        return NewFooDB(cfg, log), true
    })
}

type FooDB struct {
    cfg *config.Config
    log *slog.Logger
}

func NewFooDB(cfg *config.Config, log *slog.Logger) *FooDB {
    return &FooDB{cfg: cfg, log: log}
}

func (f *FooDB) Name() string { return "foodb" }

func (f *FooDB) Dump(ctx context.Context, w io.Writer) error {
    args := []string{
        "--host", f.cfg.DBHost,
        "--port", f.cfg.DBPort,
        "--user", f.cfg.DBUser,
        "--db",   f.cfg.DBName,
    }
    cmd := exec.CommandContext(ctx, "foodb-dump", args...)
    cmd.Env = append(cmd.Env, "FOODB_PASSWORD="+f.cfg.DBPassword)
    cmd.Stdout = w
    cmd.Stderr = newLogStderr(f.log, "foodb-dump")
    return cmd.Run()
}
```

Pontos importantes:

- **Stream pra `io.Writer`** — nunca crie arquivo intermediário; o caller
  conecta o `Writer` direto no upload pra storage (compress + upload em
  pipeline).
- **`exec.CommandContext`** — context cancellation mata o processo filho
  no shutdown ou timeout.
- **`newLogStderr`** — helper já existente em `internal/dumper/runner.go`,
  loga stderr linha-a-linha em `slog`.

---

## Passo 2: criar o restorer (se aplicável)

`internal/restorer/foodb.go`:

```go
package restorer

import (
    "context"
    "io"
    "log/slog"
    "os/exec"

    "github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
    Register("foodb", func(cfg *config.Config, log *slog.Logger) (Restorer, bool) {
        if cfg.DBType != "foodb" {
            return nil, false
        }
        return NewFooDB(cfg, log), true
    })
}

type FooDB struct{ /* ... */ }

func (f *FooDB) Name() string { return "foodb" }

func (f *FooDB) Restore(ctx context.Context, r io.Reader) error {
    args := []string{ /* ... */ }
    cmd := exec.CommandContext(ctx, "foodb-restore", args...)
    cmd.Stdin = r
    return cmd.Run()
}
```

---

## Passo 3: adicionar a CLI ao Dockerfile

Em `docker/Dockerfile`:

```dockerfile
RUN apk add --no-cache foodb-client
```

Se a CLI não está em Alpine APK:
- Tente baixar binário static do release upstream (multi-stage)
- Se requer JVM/Python/Oracle Instant Client → custom image (ver
  [docker_image.md](../operations/docker_image.md#engines-que-precisam-de-imagem-custom))

---

## Passo 4: testar

### Unit test

`internal/dumper/foodb_test.go`:

```go
func TestFooDBBuilder(t *testing.T) {
    cfg := &config.Config{
        DBType: "foodb", DBHost: "h", DBPort: "1234",
        DBUser: "u", DBPassword: "p", DBName: "d",
    }
    f := NewFooDB(cfg, slog.Default())
    if f.Name() != "foodb" {
        t.Fatalf("unexpected name: %s", f.Name())
    }
}
```

### E2E test

Adicione um teste em `e2e/foodb_test.go` usando testcontainers-go:

```go
func TestFooDB(t *testing.T) {
    ctx := context.Background()

    container, err := tcfoodb.Run(ctx, "foodb/foodb:1.0", /* ... */)
    require.NoError(t, err)
    defer container.Terminate(ctx)

    minio := startMinIO(ctx, t)
    defer minio.Terminate(ctx)

    // 1. seed dados
    seedFooDB(ctx, t, container)

    // 2. roda dumpscript dump
    runDumpscript(ctx, t, "dump", /* env vars */)

    // 3. roda dumpscript restore num DB vazio
    fresh := startFreshFooDB(ctx, t)
    runDumpscript(ctx, t, "restore", /* env vars */)

    // 4. valida que dados batem
    assertFooDBContents(ctx, t, fresh)
}
```

Veja `e2e/postgres_test.go` como template completo.

---

## Passo 5: documentar

Crie `docs/engines/foodb.md` cobrindo:

1. **Versões suportadas** (matriz cliente vs server)
2. **Env vars específicas** (se houver — a maioria reusa `DB_*` padrão)
3. **Restore caveats** (precisa de DB pré-existente? Não suporta?)
4. **Exemplo CronJob** + exemplo `BackupSchedule` operator-style

---

## Passo 6: registrar no factory? Não.

Trick principal do projeto: o `factory.go` **não tem `switch` por engine**.
Cada `init()` em `internal/dumper/<x>.go` se auto-registra na tabela
global do package — `factory.New(cfg)` itera os builders registrados e
retorna o primeiro que case com `cfg.DBType`.

Logo: importar o package basta. O blank import em `cmd/dumpscript/main.go`:

```go
import _ "github.com/cloudscript-technology/dumpscript/internal/dumper"
```

já puxa todos os engines via `init()`. Adicionar um engine novo é **zero
edits** no main e zero edits no factory.

Isso é o mesmo pattern usado em:
- `database/sql` (drivers se registram via `init()`)
- `image` package da stdlib (decoders idem)

---

## Checklist final

- [ ] `internal/dumper/foodb.go` criado com `init() → Register()`
- [ ] `internal/restorer/foodb.go` (ou ausência documentada como unsupported)
- [ ] `docker/Dockerfile` instala a CLI (ou doc explica imagem custom)
- [ ] `internal/dumper/foodb_test.go` cobre o builder
- [ ] `e2e/foodb_test.go` faz roundtrip dump+restore real
- [ ] `docs/engines/foodb.md` documenta versão / env / caveats
- [ ] `docs/README.md` index linka o novo engine
- [ ] `make test-race && make e2e-one NAME=TestFooDB` passam

---

## Back

- [Testing](../operations/testing.md)
- [Engines existentes](../engines/)
