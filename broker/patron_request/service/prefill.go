package prservice

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	lookupservice "github.com/indexdata/crosslink/broker/service"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
)

var (
	// ErrPrefillUnavailable indicates that no catalog is configured for the requester.
	ErrPrefillUnavailable = errors.New("no catalog configured for prefill")
	// ErrPrefillNotFound indicates that the catalog returned no record.
	ErrPrefillNotFound = errors.New("catalog record not found")
)

// Prefill contains only catalog metadata and the caller's optional service choice.
type Prefill struct {
	BibliographicInfo *iso18626.BibliographicInfo `json:"bibliographicInfo,omitempty"`
	PublicationInfo   *iso18626.PublicationInfo   `json:"publicationInfo,omitempty"`
	ServiceInfo       *iso18626.ServiceInfo       `json:"serviceInfo,omitempty"`
}

// PrefillService retrieves metadata without creating requests or emitting workflow events.
type PrefillService struct {
	repo      ill_db.IllRepo
	directory adapter.DirectoryLookupAdapter
	factory   *lookupservice.LookupAdapterFactory
}

// NewPrefillService uses the same requester catalog selection as request workflows.
func NewPrefillService(repo ill_db.IllRepo, directory adapter.DirectoryLookupAdapter, factory *lookupservice.LookupAdapterFactory) *PrefillService {
	return &PrefillService{repo: repo, directory: directory, factory: factory}
}

// Prefill retrieves a catalog record for a requester already authorized by the handler.
func (s *PrefillService) Prefill(ctx common.ExtendedContext, symbol string, params catalog.LookupParams) (Prefill, error) {
	peers, _, err := s.repo.GetCachedPeersBySymbols(ctx, []string{symbol}, s.directory)
	if err != nil {
		return Prefill{}, fmt.Errorf("resolve prefill requester: %w", err)
	}
	if len(peers) != 1 {
		return Prefill{}, ErrPrefillUnavailable
	}
	adapter, _, err := s.factory.GetAdapterRequester(ctx, peers[0])
	if err != nil {
		return Prefill{}, fmt.Errorf("resolve prefill catalog: %w", err)
	}
	if adapter == nil {
		return Prefill{}, ErrPrefillUnavailable
	}
	result, err := catalog.LookupContext(ctx, adapter, params)
	if err != nil {
		return Prefill{}, fmt.Errorf("prefill catalog lookup: %w", err)
	}
	if !result.HasRecord() {
		return Prefill{}, ErrPrefillNotFound
	}
	metadata, err := result.GetMetadata()
	if err != nil {
		return Prefill{}, fmt.Errorf("read prefill metadata: %w", err)
	}
	return metadataPrefill(metadata, params)
}

func metadataPrefill(metadata catalog.Metadata, params catalog.LookupParams) (Prefill, error) {
	var result Prefill
	if metadata.Identifier != "" || metadata.Title != "" || metadata.Subtitle != "" || metadata.Author != "" || metadata.Edition != "" || metadata.Isbn != "" || metadata.Issn != "" {
		result.BibliographicInfo = &iso18626.BibliographicInfo{}
		if err := catalog.MetadataRequestUpdate(result.BibliographicInfo, metadata, params, dirapi.Replace); err != nil {
			return Prefill{}, err
		}
	}
	if metadata.Publisher != "" || metadata.PublicationDate != "" || metadata.PlaceOfPublication != "" || metadata.PublicationType != "" {
		result.PublicationInfo = &iso18626.PublicationInfo{Publisher: metadata.Publisher, PublicationDate: metadata.PublicationDate, PlaceOfPublication: metadata.PlaceOfPublication}
		if metadata.PublicationType != "" {
			result.PublicationInfo.PublicationType = &iso18626.TypeSchemeValuePair{Text: metadata.PublicationType}
		}
	}
	if params.ServiceType != "" {
		result.ServiceInfo = &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceType(params.ServiceType)}
	}
	return result, nil
}
