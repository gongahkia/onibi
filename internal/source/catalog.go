package source

import (
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
)

var auditReviewedAt = time.Date(2026, time.August, 22, 0, 0, 0, 0, time.UTC)

// Catalog is the runtime policy representation of docs/research/source-audit.md.
func Catalog() []domain.SourceInfo {
	availability := domain.Capabilities{VenueDiscovery: true, Metadata: true, Facilities: true, Pricing: true, Availability: true, BookingURL: true, GeographicData: true}
	return []domain.SourceInfo{
		{ID: "local-manual", Name: "Local manual input", Operator: "kaypoh user", Policy: policy(domain.SourceManualOnly, domain.Capabilities{Availability: true, Metadata: true, Pricing: true, BookingURL: true}, "User-supplied local records; this source makes no network requests.")},
		{ID: "sportsg-facilities", Name: "SportSG facilities", Operator: "Sport Singapore", Website: "https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view", Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, PermittedHosts: []string{"api-open.data.gov.sg", "api-production.data.gov.sg", "s3.ap-southeast-1.amazonaws.com"}, Capabilities: domain.Capabilities{VenueDiscovery: true, Metadata: true, GeographicData: true, BookingURL: true}, PollFloor: 24 * time.Hour, Concurrency: 1, Timeout: 20 * time.Second, EvidenceURLs: []string{"https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view"}, ReviewedAt: auditReviewedAt}},
		{ID: "onemap", Name: "OneMap", Operator: "Singapore Land Authority", Website: "https://www.onemap.gov.sg/apidocs/", Policy: domain.SourcePolicy{Status: domain.SourceEnabledOfficialAPI, PermittedHosts: []string{"www.onemap.gov.sg"}, Capabilities: domain.Capabilities{GeographicData: true}, AuthRequired: true, PollFloor: time.Minute, Concurrency: 2, Timeout: 15 * time.Second, TermsURL: "https://www.onemap.gov.sg/legal/apitermsofservice.html", EvidenceURLs: []string{"https://www.onemap.gov.sg/apidocs/routing"}, ReviewedAt: auditReviewedAt}},
		activeSGSource(availability),
		partnerSource("onepa", "onePA", "People's Association", "https://www.onepa.gov.sg/facilities/availability", []string{"www.onepa.gov.sg"}, 15, availability),
		partnerSource("the-kallang", "The Kallang / OCBC Arena", "The Kallang Group", "https://thekallang.perfectgym.com/clientportal2/", []string{"thekallang.perfectgym.com", "change.sportshub.com.sg", "www.thekallang.com.sg"}, 30, availability),
		publicAvailabilitySource("sba-stadium", "KFF Badminton Arena @ Guillemard", "Singapore Badminton Association", "https://booking.singaporebadminton.org.sg/", []string{"booking.singaporebadminton.org.sg", "singaporebadminton.org.sg"}, 7, availability),
		publicAvailabilitySource("singapore-badminton-hall", "Singapore Badminton Hall", "Singapore Badminton Hall", "https://singaporebadmintonhall.com/book-now/", []string{"singaporebadmintonhall.com", "playtomic.com"}, 7, availability),
		publicAvailabilitySource("smash-arena", "Smash Arena", "Smash Arena", "https://booking.smasharena.sg/", []string{"smasharena.sg", "booking.smasharena.sg"}, 1, availability),
		publicAvailabilitySource("wyse-active", "Wyse Active Hub", "Wyse Active Hub / Rezerv", "https://wyseactivehub.rezerv.co/", []string{"www.wyseactivehub.com", "wyseactivehub.rezerv.co", "customer-api.rezerv.co"}, 7, availability),
		partnerSource("trusmash", "TruSmash", "Viva Capital / AFA", "https://book.afa-sports.com/scheduler", []string{"trusmash.com.sg", "book.afa-sports.com"}, 14, availability),
	}
}

func partnerSource(id, name, operator, website string, hosts []string, maximumDays int, capabilities domain.Capabilities) domain.SourceInfo {
	return domain.SourceInfo{ID: id, Name: name, Operator: operator, Website: website, Policy: domain.SourcePolicy{Status: domain.SourceExperimental, PermittedHosts: hosts, Capabilities: capabilities, PollFloor: 30 * time.Minute, AvailabilityMaxDays: maximumDays, Concurrency: 1, Timeout: 25 * time.Second, ReviewedAt: auditReviewedAt, Notes: "Partner-authorized, read-only availability integration. API is preferred; browser access is limited to approved credentials or an imported session."}}
}

func activeSGSource(capabilities domain.Capabilities) domain.SourceInfo {
	venueList := "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"
	return domain.SourceInfo{ID: "myactivesg", Name: "ActiveSG", Operator: "Sport Singapore", Website: venueList, Policy: domain.SourcePolicy{Status: domain.SourceExperimental, PermittedHosts: []string{"www.activesgcircle.gov.sg", "activesg.gov.sg"}, Capabilities: capabilities, PollFloor: time.Hour, AvailabilityMaxDays: 15, Concurrency: 1, Timeout: 25 * time.Second, EvidenceURLs: []string{"https://www.activesgcircle.gov.sg/facilities/badminton", venueList}, ReviewedAt: auditReviewedAt, Notes: "Partner-authorized, read-only ActiveSG badminton availability integration. The dedicated browser reader uses an imported session, clicks date cards only, and never opens a ballot, booking, checkout, payment, CAPTCHA, or OTP flow."}}
}

func publicAvailabilitySource(id, name, operator, website string, hosts []string, maximumDays int, capabilities domain.Capabilities) domain.SourceInfo {
	return domain.SourceInfo{ID: id, Name: name, Operator: operator, Website: website, Policy: domain.SourcePolicy{Status: domain.SourceEnabledPublicData, PermittedHosts: hosts, Capabilities: capabilities, PollFloor: time.Hour, AvailabilityMaxDays: maximumDays, Concurrency: 1, Timeout: 25 * time.Second, ReviewedAt: auditReviewedAt, Notes: "Built-in anonymous, read-only badminton availability reader. It performs no login, booking, payment, confirmation, CAPTCHA, or OTP action. The one-hour poll floor and bounded default window limit public booking reads."}}
}

func policy(status domain.SourceStatus, capabilities domain.Capabilities, notes string) domain.SourcePolicy {
	return domain.SourcePolicy{Status: status, Capabilities: capabilities, PollFloor: 10 * time.Minute, Concurrency: 0, Timeout: 15 * time.Second, ReviewedAt: auditReviewedAt, Notes: notes}
}
