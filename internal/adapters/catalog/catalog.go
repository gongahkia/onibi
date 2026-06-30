package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

type Kind string

const (
	KindAgent Kind = "agent"
	KindShell Kind = "shell"
)

type Adapter struct {
	Name              string
	Install           func(context.Context, *store.DB, string) error
	Uninstall         func(context.Context, *store.DB) error
	Status            func(context.Context, *store.DB) common.Info
	Verify            func(context.Context, *store.DB) error
	Adopt             func(context.Context, *store.DB) error
	ExpectedHooks     func(string) ([]common.ExpectedHook, error)
	ObservedHooks     func() ([]common.ObservedHook, error)
	TrustInstructions func() []string
	BackupPath        func(context.Context, *store.DB) string
}

type Manifest struct {
	Name            string
	Version         string
	Kind            Kind
	CmdPattern      map[string]string
	HookInstall     []string
	HookUninstall   []string
	RiskOverrides   map[string]string
	MinOnibiVersion string
	Adapter         Adapter
}

type Registry interface {
	Register(Manifest) error
	List() []Manifest
	Get(string) (Manifest, error)
}

type memoryRegistry struct {
	mu        sync.RWMutex
	manifests map[string]Manifest
}

var defaultRegistry = NewRegistry()

func NewRegistry() Registry {
	return &memoryRegistry{manifests: map[string]Manifest{}}
}

func Register(m Manifest) error {
	return defaultRegistry.Register(m)
}

func MustRegister(m Manifest) {
	if err := Register(m); err != nil {
		panic(err)
	}
}

func List() []Manifest {
	return defaultRegistry.List()
}

func Get(name string) (Manifest, error) {
	return defaultRegistry.Get(name)
}

func BuiltinAgentManifest(name string, adapter Adapter, cmdPattern map[string]string) Manifest {
	return Manifest{
		Name:            name,
		Version:         common.IntegrationVersion,
		Kind:            KindAgent,
		CmdPattern:      cmdPattern,
		HookInstall:     []string{"onibi install-hooks --agent " + name},
		HookUninstall:   []string{"onibi uninstall --agent " + name + " --yes"},
		RiskOverrides:   map[string]string{},
		MinOnibiVersion: "0.3.0",
		Adapter:         adapter,
	}
}

func (r *memoryRegistry) Register(m Manifest) error {
	name := normalizeName(m.Name)
	if name == "" {
		return errors.New("adapter manifest name required")
	}
	switch m.Kind {
	case KindAgent, KindShell:
	default:
		return fmt.Errorf("adapter %q has invalid kind %q", name, m.Kind)
	}
	m.Name = name
	if m.Adapter.Name == "" {
		m.Adapter.Name = name
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.manifests[name]; ok {
		return fmt.Errorf("adapter %q already registered", name)
	}
	r.manifests[name] = cloneManifest(m)
	return nil
}

func (r *memoryRegistry) List() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Manifest, 0, len(r.manifests))
	for _, m := range r.manifests {
		out = append(out, cloneManifest(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *memoryRegistry) Get(name string) (Manifest, error) {
	name = normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.manifests[name]
	if !ok {
		return Manifest{}, fmt.Errorf("adapter %q not registered", name)
	}
	return cloneManifest(m), nil
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func cloneManifest(m Manifest) Manifest {
	m.CmdPattern = cloneStringMap(m.CmdPattern)
	m.HookInstall = append([]string(nil), m.HookInstall...)
	m.HookUninstall = append([]string(nil), m.HookUninstall...)
	m.RiskOverrides = cloneStringMap(m.RiskOverrides)
	return m
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
