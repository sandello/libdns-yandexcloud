// Package yandexcloud implements a DNS record management client compatible
// with the libdns interfaces for Yandex Cloud DNS.
package yandexcloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libdns/libdns"
	dns "github.com/yandex-cloud/go-genproto/yandex/cloud/dns/v1"
	dnssdk "github.com/yandex-cloud/go-sdk/services/dns/v1"
	ycsdk "github.com/yandex-cloud/go-sdk/v2"
	"github.com/yandex-cloud/go-sdk/v2/credentials"
	"github.com/yandex-cloud/go-sdk/v2/pkg/options"
)

const (
	defaultPageSize     = 1000
	metadataFlavorKey   = "Metadata-Flavor"
	metadataFlavorValue = "Google"
)

var (
	metadataFolderIDURL = "http://169.254.169.254/computeMetadata/v1/instance/vendor/folder-id"
	metadataHTTPClient  = &http.Client{Timeout: 2 * time.Second}
)

// Provider facilitates DNS record manipulation with Yandex Cloud DNS.
type Provider struct {
	// IAMToken is a Yandex Cloud IAM token used for API authentication.
	IAMToken string `json:"iam_token,omitempty"`

	// FolderID is the Yandex Cloud folder ID containing the DNS zones. It is
	// required when using IAMToken. When UseInstanceServiceAccount is enabled,
	// it is optional and defaults to the instance metadata folder ID.
	FolderID string `json:"folder_id,omitempty"`

	// UseInstanceServiceAccount enables authentication with the service account
	// attached to the current Yandex Cloud Compute instance.
	UseInstanceServiceAccount bool `json:"use_instance_service_account,omitempty"`

	mu      sync.Mutex
	client  dnssdk.DnsZoneClient
	zoneIDs map[string]string

	writeMu sync.Mutex
}

// ListZones lists all DNS zones in the configured folder.
func (p *Provider) ListZones(ctx context.Context) ([]libdns.Zone, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	folderID := p.FolderID

	var zones []libdns.Zone
	var pageToken string
	for {
		resp, err := client.List(ctx, &dns.ListDnsZonesRequest{
			FolderId:  folderID,
			PageSize:  defaultPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list dns zones: %w", err)
		}

		for _, zone := range resp.GetDnsZones() {
			zones = append(zones, libdns.Zone{Name: normalizeZone(zone.GetZone())})
		}

		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return zones, nil
		}
	}
}

// GetRecords lists all records in the zone.
func (p *Provider) GetRecords(ctx context.Context, zone string) ([]libdns.Record, error) {
	zone = normalizeZone(zone)
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	zoneID, err := p.getZoneID(ctx, client, zone)
	if err != nil {
		return nil, err
	}

	var records []libdns.Record
	err = forEachRecordSet(ctx, client, zoneID, func(recordSet *dns.RecordSet) error {
		recordSetRecords, err := recordsFromRecordSet(zone, recordSet)
		if err != nil {
			return err
		}
		records = append(records, recordSetRecords...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get records for zone %q: %w", zone, err)
	}
	return records, nil
}

// AppendRecords adds records to the zone. It returns the records that were added.
func (p *Provider) AppendRecords(ctx context.Context, zone string, records []libdns.Record) ([]libdns.Record, error) {
	if len(records) == 0 {
		return nil, nil
	}

	zone = normalizeZone(zone)
	recordSets, returnedRecords, err := replacementRecordSetsFromRecords(zone, records)
	if err != nil {
		return nil, err
	}

	client, zoneID, err := p.clientAndZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if err := validateAppendRecordSetTTLs(ctx, client, zoneID, zone, recordSets); err != nil {
		return nil, err
	}

	op, err := client.UpsertRecordSets(ctx, &dns.UpsertRecordSetsRequest{
		DnsZoneId: zoneID,
		Merges:    recordSets,
	})
	if err != nil {
		return nil, fmt.Errorf("append record sets in zone %q: %w", zone, err)
	}
	if _, err := op.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for append record sets in zone %q: %w", zone, err)
	}

	return returnedRecords, nil
}

// SetRecords sets the records in the zone, either by updating existing records or creating new ones.
// It returns the updated records.
func (p *Provider) SetRecords(ctx context.Context, zone string, records []libdns.Record) ([]libdns.Record, error) {
	if len(records) == 0 {
		return nil, nil
	}

	zone = normalizeZone(zone)
	recordSets, returnedRecords, err := replacementRecordSetsFromRecords(zone, records)
	if err != nil {
		return nil, err
	}

	client, zoneID, err := p.clientAndZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	op, err := client.UpsertRecordSets(ctx, &dns.UpsertRecordSetsRequest{
		DnsZoneId:    zoneID,
		Replacements: recordSets,
	})
	if err != nil {
		return nil, fmt.Errorf("set record sets in zone %q: %w", zone, err)
	}
	if _, err := op.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for set record sets in zone %q: %w", zone, err)
	}

	return returnedRecords, nil
}

// DeleteRecords deletes the specified records from the zone. It returns the records that were deleted.
func (p *Provider) DeleteRecords(ctx context.Context, zone string, records []libdns.Record) ([]libdns.Record, error) {
	if len(records) == 0 {
		return nil, nil
	}

	zone = normalizeZone(zone)
	client, zoneID, err := p.clientAndZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	existing, err := p.GetRecords(ctx, zone)
	if err != nil {
		return nil, err
	}

	toDelete := matchingRecords(existing, records)
	if len(toDelete) == 0 {
		return nil, nil
	}

	recordSets, deletedRecords, err := recordSetsFromRecords(zone, toDelete)
	if err != nil {
		return nil, err
	}

	op, err := client.UpsertRecordSets(ctx, &dns.UpsertRecordSetsRequest{
		DnsZoneId: zoneID,
		Deletions: recordSets,
	})
	if err != nil {
		return nil, fmt.Errorf("delete record sets in zone %q: %w", zone, err)
	}
	if _, err := op.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for delete record sets in zone %q: %w", zone, err)
	}

	return deletedRecords, nil
}

func (p *Provider) clientAndZoneID(ctx context.Context, zone string) (dnssdk.DnsZoneClient, string, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, "", err
	}
	zoneID, err := p.getZoneID(ctx, client, zone)
	if err != nil {
		return nil, "", err
	}
	return client, zoneID, nil
}

func (p *Provider) getClient(ctx context.Context) (dnssdk.DnsZoneClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	creds, err := p.sdkCredentials(ctx)
	if err != nil {
		return nil, err
	}

	sdk, err := ycsdk.Build(ctx,
		options.WithCredentials(creds),
		options.WithDefaultRetryOptions(),
	)
	if err != nil {
		return nil, fmt.Errorf("build yandex cloud sdk: %w", err)
	}

	p.client = dnssdk.NewDnsZoneClient(sdk)
	p.zoneIDs = make(map[string]string)
	return p.client, nil
}

func (p *Provider) sdkCredentials(ctx context.Context) (credentials.Credentials, error) {
	if p.IAMToken == "" && !p.UseInstanceServiceAccount {
		return nil, errors.New("iam_token or use_instance_service_account is required")
	}
	if p.IAMToken != "" && p.UseInstanceServiceAccount {
		return nil, errors.New("iam_token and use_instance_service_account are mutually exclusive")
	}
	if p.FolderID == "" {
		if !p.UseInstanceServiceAccount {
			return nil, errors.New("folder_id is required")
		}
		folderID, err := instanceMetadataFolderID(ctx)
		if err != nil {
			return nil, err
		}
		p.FolderID = folderID
	}
	if p.UseInstanceServiceAccount {
		return credentials.InstanceServiceAccount(), nil
	}
	return credentials.IAMToken(p.IAMToken), nil
}

func validateAppendRecordSetTTLs(ctx context.Context, client dnssdk.DnsZoneClient, zoneID, zone string, recordSets []*dns.RecordSet) error {
	if len(recordSets) == 0 {
		return nil
	}

	appendTTLByKey := make(map[nameTypeKey]int64, len(recordSets))
	for _, recordSet := range recordSets {
		appendTTLByKey[nameTypeKey{
			name: recordSet.GetName(),
			typ:  strings.ToUpper(recordSet.GetType()),
		}] = recordSet.GetTtl()
	}

	err := forEachRecordSet(ctx, client, zoneID, func(recordSet *dns.RecordSet) error {
		return validateAppendRecordSetTTLMatch(recordSet, appendTTLByKey)
	})
	if err != nil {
		return fmt.Errorf("check append ttl in zone %q: %w", zone, err)
	}
	return nil
}

func forEachRecordSet(ctx context.Context, client dnssdk.DnsZoneClient, zoneID string, visit func(*dns.RecordSet) error) error {
	var pageToken string
	for {
		resp, err := client.ListRecordSets(ctx, &dns.ListDnsZoneRecordSetsRequest{
			DnsZoneId: zoneID,
			PageSize:  defaultPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return fmt.Errorf("list record sets: %w", err)
		}
		for _, recordSet := range resp.GetRecordSets() {
			if err := visit(recordSet); err != nil {
				return err
			}
		}
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return nil
		}
	}
}

func validateAppendRecordSetTTLMatch(recordSet *dns.RecordSet, appendTTLByKey map[nameTypeKey]int64) error {
	key := nameTypeKey{
		name: recordSet.GetName(),
		typ:  strings.ToUpper(recordSet.GetType()),
	}
	appendTTL, ok := appendTTLByKey[key]
	if !ok || recordSet.GetTtl() == appendTTL {
		return nil
	}
	return fmt.Errorf("cannot append %s %s with ttl %d: existing record set ttl is %d", key.name, key.typ, appendTTL, recordSet.GetTtl())
}

func instanceMetadataFolderID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataFolderIDURL, nil)
	if err != nil {
		return "", fmt.Errorf("create metadata folder-id request: %w", err)
	}
	req.Header.Set(metadataFlavorKey, metadataFlavorValue)

	resp, err := metadataHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get folder_id from instance metadata: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read folder_id from instance metadata: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("get folder_id from instance metadata: status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	folderID := strings.TrimSpace(string(body))
	if folderID == "" {
		return "", errors.New("get folder_id from instance metadata: empty response")
	}
	return folderID, nil
}

func (p *Provider) getZoneID(ctx context.Context, client dnssdk.DnsZoneClient, zone string) (string, error) {
	p.mu.Lock()
	if zoneID := p.zoneIDs[zone]; zoneID != "" {
		p.mu.Unlock()
		return zoneID, nil
	}
	p.mu.Unlock()

	folderID := p.FolderID
	var pageToken string
	for {
		resp, err := client.List(ctx, &dns.ListDnsZonesRequest{
			FolderId:  folderID,
			PageSize:  defaultPageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return "", fmt.Errorf("list dns zones: %w", err)
		}

		for _, dnsZone := range resp.GetDnsZones() {
			if normalizeZone(dnsZone.GetZone()) != zone {
				continue
			}

			zoneID := dnsZone.GetId()
			p.mu.Lock()
			p.zoneIDs[zone] = zoneID
			p.mu.Unlock()
			return zoneID, nil
		}

		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			return "", fmt.Errorf("dns zone %q not found in folder %q", zone, folderID)
		}
	}
}

func recordsFromRecordSet(zone string, recordSet *dns.RecordSet) ([]libdns.Record, error) {
	var records []libdns.Record
	for _, data := range recordSet.GetData() {
		recordType := strings.ToUpper(recordSet.GetType())
		if recordType == "TXT" {
			data = unquoteTXT(data)
		}
		rr := libdns.RR{
			Name: libdns.RelativeName(recordSet.GetName(), zone),
			TTL:  time.Duration(recordSet.GetTtl()) * time.Second,
			Type: recordType,
			Data: data,
		}
		record, err := rr.Parse()
		if err != nil {
			return nil, fmt.Errorf("parse record set %s %s: %w", recordSet.GetName(), recordType, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func recordSetsFromRecords(zone string, records []libdns.Record) ([]*dns.RecordSet, []libdns.Record, error) {
	groups := make(map[recordSetKey]*dns.RecordSet)
	var returnedRecords []libdns.Record

	for _, record := range records {
		rr := record.RR()
		parsed, err := rr.Parse()
		if err != nil {
			return nil, nil, fmt.Errorf("parse record %+v: %w", rr, err)
		}
		returnedRecords = append(returnedRecords, parsed)

		key := recordSetKey{
			name: libdns.AbsoluteName(rr.Name, zone),
			typ:  strings.ToUpper(rr.Type),
			ttl:  int64(rr.TTL / time.Second),
		}
		if key.name == "" || key.typ == "" {
			return nil, nil, fmt.Errorf("record name and type are required: %+v", rr)
		}

		recordSet := groups[key]
		if recordSet == nil {
			recordSet = &dns.RecordSet{
				Name: key.name,
				Type: key.typ,
				Ttl:  key.ttl,
			}
			groups[key] = recordSet
		}
		recordSet.Data = append(recordSet.Data, dataForYandex(rr))
	}

	return recordSetValues(groups), returnedRecords, nil
}

func replacementRecordSetsFromRecords(zone string, records []libdns.Record) ([]*dns.RecordSet, []libdns.Record, error) {
	groups := make(map[nameTypeKey]*dns.RecordSet)
	var returnedRecords []libdns.Record

	for _, record := range records {
		rr := record.RR()
		parsed, err := rr.Parse()
		if err != nil {
			return nil, nil, fmt.Errorf("parse record %+v: %w", rr, err)
		}
		returnedRecords = append(returnedRecords, parsed)

		key := nameTypeKey{
			name: libdns.AbsoluteName(rr.Name, zone),
			typ:  strings.ToUpper(rr.Type),
		}
		if key.name == "" || key.typ == "" {
			return nil, nil, fmt.Errorf("record name and type are required: %+v", rr)
		}

		ttl := int64(rr.TTL / time.Second)
		recordSet := groups[key]
		if recordSet == nil {
			recordSet = &dns.RecordSet{
				Name: key.name,
				Type: key.typ,
				Ttl:  ttl,
			}
			groups[key] = recordSet
		} else if recordSet.Ttl != ttl {
			return nil, nil, fmt.Errorf("records for %s %s have mixed TTL values", key.name, key.typ)
		}
		recordSet.Data = append(recordSet.Data, dataForYandex(rr))
	}

	return recordSetValues(groups), returnedRecords, nil
}

func recordSetValues[K comparable](groups map[K]*dns.RecordSet) []*dns.RecordSet {
	recordSets := make([]*dns.RecordSet, 0, len(groups))
	for _, recordSet := range groups {
		recordSets = append(recordSets, recordSet)
	}
	return recordSets
}

func matchingRecords(existing, filters []libdns.Record) []libdns.Record {
	var matches []libdns.Record
	seen := make(map[libdns.RR]struct{})

	for _, existingRecord := range existing {
		existingRR := existingRecord.RR()
		for _, filter := range filters {
			if recordMatches(existingRR, filter.RR()) {
				if _, ok := seen[existingRR]; !ok {
					matches = append(matches, existingRecord)
					seen[existingRR] = struct{}{}
				}
				break
			}
		}
	}
	return matches
}

func recordMatches(existing, filter libdns.RR) bool {
	if existing.Name != filter.Name {
		return false
	}
	if filter.Type != "" && existing.Type != strings.ToUpper(filter.Type) {
		return false
	}
	if filter.TTL != 0 && existing.TTL != filter.TTL {
		return false
	}
	if filter.Data != "" && existing.Data != filter.Data {
		return false
	}
	return true
}

func dataForYandex(rr libdns.RR) string {
	if strings.ToUpper(rr.Type) == "TXT" {
		return strconv.Quote(rr.Data)
	}
	return rr.Data
}

func unquoteTXT(data string) string {
	unquoted, err := strconv.Unquote(data)
	if err == nil {
		return unquoted
	}
	return data
}

func normalizeZone(zone string) string {
	if zone == "" || strings.HasSuffix(zone, ".") {
		return zone
	}
	return zone + "."
}

type recordSetKey struct {
	name string
	typ  string
	ttl  int64
}

type nameTypeKey struct {
	name string
	typ  string
}

// Interface guards
var (
	_ libdns.RecordGetter   = (*Provider)(nil)
	_ libdns.RecordAppender = (*Provider)(nil)
	_ libdns.RecordSetter   = (*Provider)(nil)
	_ libdns.RecordDeleter  = (*Provider)(nil)
	_ libdns.ZoneLister     = (*Provider)(nil)
)
