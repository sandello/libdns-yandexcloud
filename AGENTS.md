# Repository Guidelines

This repository implements a libdns provider for Yandex Cloud DNS.

## Development

- Keep the module path as `github.com/sandello/libdns-yandexcloud`.
- Use the official Yandex Cloud SDK DNS service from `github.com/yandex-cloud/go-sdk/services/dns/v1`.
- Preserve libdns semantics when mapping records to Yandex Cloud RRsets. Append operations must not mutate TTLs of existing RRsets.
- Follow existing provider patterns and keep changes small.
- Run `gofmt` on changed Go files.

## Testing

Run unit tests from the repository root:

```sh
go test ./...
```

Run live libdns tests from `libdnstest/` only after configuring local credentials:

```sh
set -a; . ./.env; set +a; go test -v ./...
```

Do not commit secrets or local `.env` files.
