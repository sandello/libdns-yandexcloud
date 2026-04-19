# Yandex Cloud DNS for libdns

This package implements the [libdns](https://github.com/libdns/libdns) interfaces for Yandex Cloud DNS.

## Configuration

```go
provider := &yandexcloud.Provider{
	IAMToken: "...",
	FolderID: "...",
}
```

With a user account key file:

```go
provider := &yandexcloud.Provider{
	UserAccountKeyFilePath: "...",
	FolderID:               "...",
}
```

With a service account key file:

```go
provider := &yandexcloud.Provider{
	ServiceAccountKeyFilePath: "...",
	FolderID:                  "...",
}
```

For workloads running on a Yandex Cloud Compute instance with an attached service account:

```go
provider := &yandexcloud.Provider{
	UseInstanceServiceAccount: true,
}
```

Configure exactly one authentication method: `IAMToken`, `UserAccountKeyFilePath`, `ServiceAccountKeyFilePath`, or `UseInstanceServiceAccount`. `FolderID` is required for all authentication methods except `UseInstanceServiceAccount`, where it is optional and defaults to the instance metadata folder ID.

Record changes are submitted through Yandex Cloud DNS record set operations and waited on before the libdns method returns.
