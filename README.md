# Yandex Cloud DNS for libdns

This package implements the [libdns](https://github.com/libdns/libdns) interfaces for Yandex Cloud DNS.

## Configuration

```go
provider := &yandexcloud.Provider{
	IAMToken: "...",
	FolderID: "...",
}
```

For workloads running on a Yandex Cloud Compute instance with an attached service account:

```go
provider := &yandexcloud.Provider{
	UseInstanceServiceAccount: true,
}
```

When using `IAMToken`, `FolderID` is required because the Yandex Cloud DNS API lists DNS zones by folder. When using `UseInstanceServiceAccount`, `FolderID` is optional and defaults to the instance metadata folder ID. Configure exactly one authentication method: either `IAMToken` or `UseInstanceServiceAccount`.

Record changes are submitted through Yandex Cloud DNS record set operations and waited on before the libdns method returns.
