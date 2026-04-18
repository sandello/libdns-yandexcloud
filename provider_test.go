package yandexcloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/libdns/libdns"
	dns "github.com/yandex-cloud/go-genproto/yandex/cloud/dns/v1"
)

func TestRecordsFromRecordSet(t *testing.T) {
	records, err := recordsFromRecordSet("example.com.", &dns.RecordSet{
		Name: "www.example.com.",
		Type: "A",
		Ttl:  300,
		Data: []string{"192.0.2.1", "192.0.2.2"},
	})
	if err != nil {
		t.Fatalf("recordsFromRecordSet failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	for _, record := range records {
		address, ok := record.(libdns.Address)
		if !ok {
			t.Fatalf("expected libdns.Address, got %T", record)
		}
		if address.Name != "www" {
			t.Fatalf("expected relative name www, got %q", address.Name)
		}
		if address.TTL != 300*time.Second {
			t.Fatalf("expected ttl 300s, got %s", address.TTL)
		}
	}
}

func TestRecordsFromRecordSetUnquotesTXT(t *testing.T) {
	records, err := recordsFromRecordSet("example.com.", &dns.RecordSet{
		Name: "example.com.",
		Type: "TXT",
		Ttl:  300,
		Data: []string{`"Hello, world!"`},
	})
	if err != nil {
		t.Fatalf("recordsFromRecordSet failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	txt, ok := records[0].(libdns.TXT)
	if !ok {
		t.Fatalf("expected libdns.TXT, got %T", records[0])
	}
	if txt.Name != "@" {
		t.Fatalf("expected root name, got %q", txt.Name)
	}
	if txt.Text != "Hello, world!" {
		t.Fatalf("expected unquoted text, got %q", txt.Text)
	}
}

func TestRecordsFromRecordSetRejectsInvalidData(t *testing.T) {
	_, err := recordsFromRecordSet("example.com.", &dns.RecordSet{
		Name: "www.example.com.",
		Type: "A",
		Ttl:  300,
		Data: []string{"not-an-ip"},
	})
	if err == nil {
		t.Fatal("expected invalid record data error")
	}
}

func TestRecordSetsFromRecordsGroupsValues(t *testing.T) {
	recordSets, returnedRecords, err := recordSetsFromRecords("example.com.", []libdns.Record{
		libdns.Address{Name: "www", TTL: 300 * time.Second, IP: netip.MustParseAddr("192.0.2.1")},
		libdns.Address{Name: "www", TTL: 300 * time.Second, IP: netip.MustParseAddr("192.0.2.2")},
		libdns.TXT{Name: "@", TTL: 600 * time.Second, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("recordSetsFromRecords failed: %v", err)
	}
	if len(returnedRecords) != 3 {
		t.Fatalf("expected 3 returned records, got %d", len(returnedRecords))
	}
	if len(recordSets) != 2 {
		t.Fatalf("expected 2 record sets, got %d", len(recordSets))
	}

	aSet := findRecordSet(recordSets, "www.example.com.", "A")
	if aSet == nil {
		t.Fatal("missing A record set")
	}
	if aSet.Ttl != 300 {
		t.Fatalf("expected A ttl 300, got %d", aSet.Ttl)
	}
	if !sameStrings(aSet.Data, []string{"192.0.2.1", "192.0.2.2"}) {
		t.Fatalf("unexpected A data: %#v", aSet.Data)
	}

	txtSet := findRecordSet(recordSets, "example.com.", "TXT")
	if txtSet == nil {
		t.Fatal("missing TXT record set")
	}
	if !sameStrings(txtSet.Data, []string{`"hello"`}) {
		t.Fatalf("unexpected TXT data: %#v", txtSet.Data)
	}
}

func TestReplacementRecordSetsRejectMixedTTL(t *testing.T) {
	_, _, err := replacementRecordSetsFromRecords("example.com.", []libdns.Record{
		libdns.TXT{Name: "www", TTL: 300 * time.Second, Text: "one"},
		libdns.TXT{Name: "www", TTL: 600 * time.Second, Text: "two"},
	})
	if err == nil {
		t.Fatal("expected mixed TTL error")
	}
}

func TestValidateAppendRecordSetTTLMatchAllowsMatchingTTL(t *testing.T) {
	err := validateAppendRecordSetTTLMatch(&dns.RecordSet{Name: "www.example.com.", Type: "A", Ttl: 300}, map[nameTypeKey]int64{
		{name: "www.example.com.", typ: "A"}: 300,
	})
	if err != nil {
		t.Fatalf("expected matching ttl to be allowed, got %v", err)
	}
}

func TestValidateAppendRecordSetTTLMatchRejectsMismatchedTTL(t *testing.T) {
	err := validateAppendRecordSetTTLMatch(&dns.RecordSet{Name: "www.example.com.", Type: "A", Ttl: 300}, map[nameTypeKey]int64{
		{name: "www.example.com.", typ: "A"}: 600,
	})
	if err == nil {
		t.Fatal("expected mismatched ttl error")
	}
}

func TestValidateAppendRecordSetTTLMatchIgnoresUnrelatedRecordSets(t *testing.T) {
	appendTTLByKey := map[nameTypeKey]int64{
		{name: "www.example.com.", typ: "A"}: 600,
	}
	for _, recordSet := range []*dns.RecordSet{
		{Name: "api.example.com.", Type: "A", Ttl: 300},
		{Name: "www.example.com.", Type: "TXT", Ttl: 300},
	} {
		if err := validateAppendRecordSetTTLMatch(recordSet, appendTTLByKey); err != nil {
			t.Fatalf("expected unrelated record sets to be ignored, got %v", err)
		}
	}
}

func TestMatchingRecordsHonorsDeleteWildcards(t *testing.T) {
	existing := []libdns.Record{
		libdns.TXT{Name: "keep", TTL: 300 * time.Second, Text: "one"},
		libdns.TXT{Name: "delete", TTL: 300 * time.Second, Text: "one"},
		libdns.TXT{Name: "delete", TTL: 600 * time.Second, Text: "two"},
		libdns.Address{Name: "delete", TTL: 300 * time.Second, IP: netip.MustParseAddr("192.0.2.1")},
	}

	matches := matchingRecords(existing, []libdns.Record{
		libdns.RR{Name: "delete", Type: "TXT"},
	})

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	for _, match := range matches {
		rr := match.RR()
		if rr.Name != "delete" || rr.Type != "TXT" {
			t.Fatalf("unexpected match: %+v", rr)
		}
	}
}

func TestInstanceMetadataFolderID(t *testing.T) {
	var gotMetadataFlavor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMetadataFlavor = r.Header.Get(metadataFlavorKey)
		_, _ = w.Write([]byte(" b1gtestfolder \n"))
	}))
	defer server.Close()

	restoreMetadata := replaceMetadataClientForTest(t, server.URL, server.Client())
	defer restoreMetadata()

	folderID, err := instanceMetadataFolderID(context.Background())
	if err != nil {
		t.Fatalf("instanceMetadataFolderID failed: %v", err)
	}
	if folderID != "b1gtestfolder" {
		t.Fatalf("expected trimmed folder id, got %q", folderID)
	}
	if gotMetadataFlavor != metadataFlavorValue {
		t.Fatalf("expected Metadata-Flavor Google header, got %q", gotMetadataFlavor)
	}
}

func TestInstanceMetadataFolderIDRejectsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(" \n"))
	}))
	defer server.Close()

	restoreMetadata := replaceMetadataClientForTest(t, server.URL, server.Client())
	defer restoreMetadata()

	if _, err := instanceMetadataFolderID(context.Background()); err == nil {
		t.Fatal("expected empty metadata response error")
	}
}

func replaceMetadataClientForTest(t *testing.T, url string, client *http.Client) func() {
	t.Helper()
	oldURL := metadataFolderIDURL
	oldClient := metadataHTTPClient
	metadataFolderIDURL = url
	metadataHTTPClient = client
	return func() {
		metadataFolderIDURL = oldURL
		metadataHTTPClient = oldClient
	}
}

func findRecordSet(recordSets []*dns.RecordSet, name, typ string) *dns.RecordSet {
	for _, recordSet := range recordSets {
		if recordSet.Name == name && recordSet.Type == typ {
			return recordSet
		}
	}
	return nil
}

func sameStrings(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
