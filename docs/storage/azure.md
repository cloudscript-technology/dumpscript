# Azure Blob backend

Covers real Azure Blob Storage and the local
[Azurite](https://github.com/Azure/Azurite) emulator.
`STORAGE_BACKEND=azure`.

---

## Env vars

| Var | Notes |
|---|---|
| `AZURE_STORAGE_ACCOUNT` | required |
| `AZURE_STORAGE_KEY` | Shared Key (or use SAS) |
| `AZURE_STORAGE_SAS_TOKEN` | SAS token — exclusive with Key |
| `AZURE_STORAGE_CONTAINER` | required |
| `AZURE_STORAGE_PREFIX` | falls back to `S3_PREFIX` if unset |
| `AZURE_STORAGE_ENDPOINT` | override for Azurite / Gov clouds |
| `S3_KEY` | restore only — blob name, same var as S3 |

---

## Real Azure — Shared Key

```sh
-e STORAGE_BACKEND=azure
-e AZURE_STORAGE_ACCOUNT=myaccount
-e AZURE_STORAGE_KEY=$(az storage account keys list \
     --account-name myaccount --query '[0].value' -o tsv)
-e AZURE_STORAGE_CONTAINER=dumps
-e AZURE_STORAGE_PREFIX=postgresql-dumps
```

## Real Azure — SAS token

Preferred for least-privilege:

```sh
-e AZURE_STORAGE_SAS_TOKEN='?sv=2022-11-02&ss=b&srt=co&sp=rwdlac...'
```

Required SAS permissions: `r`, `w`, `d`, `l` on the container.

---

## Azurite (local dev)

The `Eby8...` key below is Azurite's **well-known public** development key —
safe to hard-code locally, never use in production.

```sh
# Start Azurite
podman run -d --name azurite -p 10000:10000 \
  mcr.microsoft.com/azure-storage/azurite:latest \
  azurite-blob --blobHost 0.0.0.0 --loose

# Create container via az CLI
export AZURE_STORAGE_CONNECTION_STRING="DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://localhost:10000/devstoreaccount1;"
az storage container create --name dumps

# Point dumpscript at it
podman run --rm \
  -e STORAGE_BACKEND=azure \
  -e AZURE_STORAGE_ACCOUNT=devstoreaccount1 \
  -e AZURE_STORAGE_KEY="Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==" \
  -e AZURE_STORAGE_CONTAINER=dumps \
  -e AZURE_STORAGE_ENDPOINT=http://host.containers.internal:10000/devstoreaccount1 \
  -e DB_TYPE=... localhost/dumpscript:go-alpine dump
```

---

## Government / sovereign clouds

```sh
-e AZURE_STORAGE_ENDPOINT=https://myaccount.blob.core.usgovcloudapi.net
```

Overrides the default `.blob.core.windows.net` URL.

---

## Upload tuning

Same env vars as S3:

- `STORAGE_UPLOAD_CUTOFF` — threshold for block upload (default `200M`)
- `STORAGE_CHUNK_SIZE` — block size (default `100M`)
- `STORAGE_UPLOAD_CONCURRENCY` — parallel workers (default `4`)

Azure's block blob limit is 50 000 blocks, so the `100M` default scales to
~5 TB per blob.

---

## Restore — find a blob

```sh
az storage blob list \
  --account-name myaccount --container-name dumps \
  --prefix postgresql-dumps/daily/ \
  --query "[].name" -o tsv | sort | tail -5
```

---

## Back

- [Storage overview](./README.md)
- [Docs home](../README.md)
