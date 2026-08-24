package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingUsesDefaults(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Sources["sportsg-facilities"].Enabled {
		t.Fatal("expected SportSG source enabled by default")
	}
	if config.Sources["onepa"].Enabled {
		t.Fatal("partner availability sources must require explicit enablement")
	}
	for _, sourceID := range []string{"sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active"} {
		if !config.Sources[sourceID].Enabled {
			t.Fatalf("%s must be enabled by default", sourceID)
		}
	}
	if config.Sources["smash-arena"].AvailabilityMaxDays != 1 {
		t.Fatalf("Smash Arena default horizon = %d, want 1", config.Sources["smash-arena"].AvailabilityMaxDays)
	}
	if config.Daemon.RefreshMinutes != 30 {
		t.Fatalf("default refresh = %d, want 30 minutes", config.Daemon.RefreshMinutes)
	}
}

func TestLoadExampleStyleConfigKeepsBuiltInReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte("version = 1\n[sources.onepa]\nenabled = false\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active"} {
		if !config.Sources[sourceID].Enabled {
			t.Fatalf("%s was lost while loading partial config", sourceID)
		}
	}
}

func TestResolveSecret(t *testing.T) {
	t.Setenv("KAYPOH_TEST_SECRET", "present")
	config := Config{}
	value, err := config.ResolveSecret("env:KAYPOH_TEST_SECRET")
	if err != nil || value != "present" {
		t.Fatalf("ResolveSecret() = %q, %v", value, err)
	}
	if _, err := config.ResolveSecret("env:KAYPOH_MISSING_SECRET"); err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestWriteExampleRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteExample(path); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"[sources.myactivesg.activesg]", "[sources.the-kallang.perfectgym]", "[sources.onepa.onepa]"} {
		if !strings.Contains(string(contents), section) {
			t.Fatalf("config example is missing %s", section)
		}
	}
	if err := WriteExample(path); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestValidateRejectsEnabledPartnerWithoutReadAccess(t *testing.T) {
	config, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	config.Sources["onepa"] = Source{Enabled: true}
	if err := config.Validate(); err == nil {
		t.Fatal("enabled partner without reader configuration was accepted")
	}
}

func TestValidateActiveSGReaderNeedsImportedSessionAndScope(t *testing.T) {
	config, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	config.Sources["myactivesg"] = Source{Enabled: true, ActiveSG: SourceActiveSG{Enabled: true, VenueListURL: "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"}}
	if err := config.Validate(); err == nil {
		t.Fatal("ActiveSG reader without imported session was accepted")
	}
	activeSG := config.Sources["myactivesg"]
	activeSG.ActiveSG.SessionStateBase64 = "env:KAYPOH_ACTIVESG_SESSION_STATE_B64"
	activeSG.ActiveSG.ScanAll = true
	config.Sources["myactivesg"] = activeSG
	if err := config.Validate(); err != nil {
		t.Fatalf("valid ActiveSG reader configuration = %v", err)
	}
}

func TestValidatePerfectGymReaderNeedsImportedSessionAndBadmintonType(t *testing.T) {
	config, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	config.Sources["the-kallang"] = Source{Enabled: true, PerfectGym: SourcePerfectGym{Enabled: true, AvailabilityURL: "https://thekallang.perfectgym.com/clientportal2/"}}
	if err := config.Validate(); err == nil {
		t.Fatal("PerfectGym reader without imported session was accepted")
	}
	kallang := config.Sources["the-kallang"]
	kallang.PerfectGym.SessionStateBase64 = "env:KAYPOH_THE_KALLANG_SESSION_STATE_B64"
	kallang.PerfectGym.FacilityTypeName = "Tennis Indoor Courts"
	config.Sources["the-kallang"] = kallang
	if err := config.Validate(); err == nil {
		t.Fatal("non-badminton PerfectGym facility type was accepted")
	}
	kallang.PerfectGym.FacilityTypeName = PerfectGymBadmintonFacilityType
	config.Sources["the-kallang"] = kallang
	if err := config.Validate(); err != nil {
		t.Fatalf("valid PerfectGym reader configuration = %v", err)
	}
}

func TestValidateOnePAReaderNeedsFacilityIDs(t *testing.T) {
	config, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	config.Sources["onepa"] = Source{Enabled: true, OnePA: SourceOnePA{Enabled: true}}
	if err := config.Validate(); err == nil {
		t.Fatal("onePA reader without facility IDs was accepted")
	}
	config.Sources["onepa"] = Source{Enabled: true, OnePA: SourceOnePA{Enabled: true, FacilityIDs: []string{"WoodlandsCC_BADMINTONCOURTS"}}}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid onePA reader configuration = %v", err)
	}
}

func TestLoadOnePAReaderConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte("version = 1\n[sources.onepa]\nenabled = true\n[sources.onepa.onepa]\nenabled = true\nfacility_ids = [\"WoodlandsCC_BADMINTONCOURTS\"]\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := config.Sources["onepa"].OnePA
	if !reader.Enabled || len(reader.FacilityIDs) != 1 || reader.FacilityIDs[0] != "WoodlandsCC_BADMINTONCOURTS" {
		t.Fatalf("onePA reader = %#v", reader)
	}
}
