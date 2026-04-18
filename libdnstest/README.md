# Yandex Cloud DNS libdnstest

These tests run the shared libdns provider test suite against a real Yandex Cloud DNS zone. They create, update, and delete records with names beginning with `test-`.

Use a dedicated test zone.

## Configuration

Set the environment variables from `.env.example`:

```sh
YANDEXCLOUD_FOLDER_ID=your-folder-id
YANDEXCLOUD_TEST_ZONE=example.com.
YANDEXCLOUD_IAM_TOKEN=your-iam-token
```

For a Yandex Cloud Compute instance with an attached service account:

```sh
YANDEXCLOUD_TEST_ZONE=example.com.
YANDEXCLOUD_USE_INSTANCE_SERVICE_ACCOUNT=true
```

In instance service account mode, `YANDEXCLOUD_FOLDER_ID` is optional and defaults to the instance metadata folder ID.

Run:

```sh
set -a
. ./.env
set +a
go test ./...
```
