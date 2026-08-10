package source

import (
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

var auditReviewedAt = time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)

// Catalog is the runtime policy representation of docs/research/source-audit.md.
func Catalog() []domain.SourceInfo {
	availability := domain.Capabilities{VenueDiscovery: true, SportDiscovery: true, Metadata: true, Facilities: true, Pricing: true, Availability: true, BookingURL: true, GeographicData: true}
	link := domain.Capabilities{Metadata: true, BookingURL: true}
	return []domain.SourceInfo{
		{ID: "sportsg-facilities", Name: "SportSG facilities", Operator: "Sport Singapore", Website: "https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, PermittedHosts: []string{"api-open.data.gov.sg", "api-production.data.gov.sg"}, Capabilities: domain.Capabilities{VenueDiscovery: true, Metadata: true, GeographicData: true, BookingURL: true}, PollFloor: 24 * time.Hour, Concurrency: 1, Timeout: 20 * time.Second, EvidenceURLs: []string{"https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view"}, ReviewedAt: auditReviewedAt}},
		{ID: "onemap", Name: "OneMap", Operator: "Singapore Land Authority", Website: "https://www.onemap.gov.sg/apidocs/", Policy: domain.SourcePolicy{Status: domain.SourceEnabledOfficialAPI, PermittedHosts: []string{"www.onemap.gov.sg"}, Capabilities: domain.Capabilities{GeographicData: true}, AuthRequired: true, PollFloor: time.Minute, Concurrency: 2, Timeout: 15 * time.Second, TermsURL: "https://www.onemap.gov.sg/legal/apitermsofservice.html", EvidenceURLs: []string{"https://www.onemap.gov.sg/apidocs/routing"}, ReviewedAt: auditReviewedAt}},
		{ID: "myactivesg", Name: "MyActiveSG+", Operator: "Sport Singapore", Website: "https://activesg.gov.sg/", Policy: policy(domain.SourceDisabledUnknown, link, "Availability requires a new approved public integration.")},
		{ID: "onepa", Name: "onePA", Operator: "People's Association", Website: "https://www.onepa.gov.sg/facilities/availability", Policy: policy(domain.SourceDisabledUnknown, link, "Undocumented endpoints are intentionally not used.")},
		{ID: "safra", Name: "SAFRA", Operator: "SAFRA", Website: "https://www.safra.sg/", Policy: policy(domain.SourceDisabledTerms, link, "Terms prohibit data mining, robots, and similar extraction.")},
		{ID: "playtomic", Name: "Playtomic", Operator: "Playtomic", Website: "https://playtomic.com/", Policy: policy(domain.SourceRequiresPermission, availability, "Use only after venue/partner credentials are configured.")},
		{ID: "the-kallang", Name: "The Kallang", Operator: "The Kallang Group", Website: "https://www.thekallang.com.sg/en/things-to-do/sports/pickleball.html", Policy: policy(domain.SourceManualOnly, link, "No documented read API.")},
		{ID: "sba-stadium", Name: "Singapore Badminton Stadium", Operator: "Singapore Badminton Association", Website: "https://staging.singaporebadminton.org.sg/book-a-badminton-court/", Policy: policy(domain.SourceManualOnly, link, "No documented read API.")},
		{ID: "singapore-badminton-hall", Name: "Singapore Badminton Hall", Operator: "Singapore Badminton Hall", Website: "https://staging.singaporebadminton.org.sg/book-a-badminton-court/", Policy: policy(domain.SourceManualOnly, link, "Manual booking contact only.")},
		{ID: "smash-arena", Name: "Smash Arena", Operator: "Smash Arena", Website: "https://smasharena.sg/", Policy: policy(domain.SourceDisabledUnknown, link, "No approved availability integration.")},
		{ID: "wyse-active", Name: "Wyse Active Hub", Operator: "Wyse Active Hub / Rezerv", Website: "https://www.wyseactivehub.com/", Policy: policy(domain.SourceDisabledUnknown, link, "No approved consumer API.")},
		{ID: "trusmash", Name: "TruSmash", Operator: "Viva Capital / AFA", Website: "https://trusmash.com.sg/", Policy: policy(domain.SourceDisabledUnknown, link, "No approved AFA availability integration.")},
		{ID: "oba", Name: "Optimum Badminton Academy", Operator: "OBA", Website: "https://play.google.com/store/apps/details?id=com.zencloud.oba", Policy: policy(domain.SourceManualOnly, link, "Availability is app managed.")},
		{ID: "performance-pickleball", Name: "Performance Pickleball", Operator: "Umeus", Website: "https://www.performancepickleball.org/court-booking", Policy: policy(domain.SourceManualOnly, link, "Availability is exclusively in account booking system.")},
		{ID: "play-pickle", Name: "Play! Pickle", Operator: "Play! Pickle", Website: "https://www.playpickle.sg/", Policy: policy(domain.SourceDisabledUnknown, link, "Operator policy/API not verified.")},
		{ID: "matchpoint-inc", Name: "Matchpoint Inc", Operator: "Matchpoint Inc", Website: "https://matchpointinc.com.sg/services/", Policy: policy(domain.SourceManualOnly, link, "No documented read API.")},
		{ID: "kings-pickleball", Name: "Kings Pickleball Arena", Operator: "Kings Pickleball Arena", Website: "https://kingspickleballarena.com/", Policy: policy(domain.SourceManualOnly, link, "No documented read API.")},
		{ID: "mbp-sports", Name: "MBP Sports", Operator: "MBP Sports", Website: "https://www.pickleball.sg/", Policy: policy(domain.SourceManualOnly, link, "Availability is app managed.")},
	}
}

func policy(status domain.SourceStatus, capabilities domain.Capabilities, notes string) domain.SourcePolicy {
	return domain.SourcePolicy{Status: status, Capabilities: capabilities, PollFloor: 10 * time.Minute, Concurrency: 0, Timeout: 15 * time.Second, ReviewedAt: auditReviewedAt, Notes: notes}
}
