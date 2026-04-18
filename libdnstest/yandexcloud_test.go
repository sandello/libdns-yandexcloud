package main

import (
	"os"
	"strings"
	"testing"

	"github.com/libdns/libdns/libdnstest"
	yandexcloud "github.com/sandello/libdns-yandexcloud"
)

func TestYandexCloudProvider(t *testing.T) {
	testZone := os.Getenv("YANDEXCLOUD_TEST_ZONE")
	folderID := os.Getenv("YANDEXCLOUD_FOLDER_ID")
	iamToken := os.Getenv("YANDEXCLOUD_IAM_TOKEN")
	useInstanceServiceAccount := os.Getenv("YANDEXCLOUD_USE_INSTANCE_SERVICE_ACCOUNT") == "true"

	if testZone == "" || (iamToken == "" && !useInstanceServiceAccount) {
		t.Skip("Skipping Yandex Cloud provider tests: YANDEXCLOUD_TEST_ZONE and either YANDEXCLOUD_IAM_TOKEN or YANDEXCLOUD_USE_INSTANCE_SERVICE_ACCOUNT=true must be set")
	}
	if folderID == "" && !useInstanceServiceAccount {
		t.Skip("Skipping Yandex Cloud provider tests: YANDEXCLOUD_FOLDER_ID must be set when using YANDEXCLOUD_IAM_TOKEN")
	}
	if iamToken != "" && useInstanceServiceAccount {
		t.Fatal("YANDEXCLOUD_IAM_TOKEN and YANDEXCLOUD_USE_INSTANCE_SERVICE_ACCOUNT=true are mutually exclusive")
	}
	if !strings.HasSuffix(testZone, ".") {
		t.Fatal("YANDEXCLOUD_TEST_ZONE must have a trailing dot")
	}

	provider := &yandexcloud.Provider{
		IAMToken:                  iamToken,
		FolderID:                  folderID,
		UseInstanceServiceAccount: useInstanceServiceAccount,
	}

	suite := libdnstest.NewTestSuite(provider, testZone)
	suite.RunTests(t)
}
