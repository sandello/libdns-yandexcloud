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
	userAccountKeyFilePath := os.Getenv("YANDEXCLOUD_USER_ACCOUNT_KEY_FILE_PATH")
	serviceAccountKeyFilePath := os.Getenv("YANDEXCLOUD_SERVICE_ACCOUNT_KEY_FILE_PATH")
	useInstanceServiceAccount := os.Getenv("YANDEXCLOUD_USE_INSTANCE_SERVICE_ACCOUNT") == "true"

	authMethodCount := 0
	for _, enabled := range []bool{
		iamToken != "",
		userAccountKeyFilePath != "",
		serviceAccountKeyFilePath != "",
		useInstanceServiceAccount,
	} {
		if enabled {
			authMethodCount++
		}
	}

	if testZone == "" || authMethodCount == 0 {
		t.Skip("Skipping Yandex Cloud provider tests: YANDEXCLOUD_TEST_ZONE and exactly one authentication method must be set")
	}
	if !strings.HasSuffix(testZone, ".") {
		t.Fatal("YANDEXCLOUD_TEST_ZONE must have a trailing dot")
	}

	provider := &yandexcloud.Provider{
		IAMToken:                  iamToken,
		UserAccountKeyFilePath:    userAccountKeyFilePath,
		ServiceAccountKeyFilePath: serviceAccountKeyFilePath,
		FolderID:                  folderID,
		UseInstanceServiceAccount: useInstanceServiceAccount,
	}

	suite := libdnstest.NewTestSuite(provider, testZone)
	suite.RunTests(t)
}
