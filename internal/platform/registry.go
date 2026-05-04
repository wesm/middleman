package platform

import "fmt"

type Registry struct {
	providers map[providerKey]Provider
}

type providerKey struct {
	platform Kind
	host     string
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{
		providers: make(map[providerKey]Provider, len(providers)),
	}
	for _, provider := range providers {
		if err := registry.Register(provider); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(provider Provider) error {
	if r.providers == nil {
		r.providers = make(map[providerKey]Provider)
	}

	key := providerKey{
		platform: provider.Platform(),
		host:     provider.Host(),
	}
	if _, ok := r.providers[key]; ok {
		return fmt.Errorf("provider already registered for %s/%s", key.platform, key.host)
	}
	r.providers[key] = provider
	return nil
}

func (r *Registry) Provider(kind Kind, host string) (Provider, error) {
	provider, ok := r.providers[providerKey{platform: kind, host: host}]
	if !ok {
		return nil, ProviderNotConfigured(kind, host)
	}
	return provider, nil
}

func (r *Registry) Capabilities(kind Kind, host string) (Capabilities, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return Capabilities{}, err
	}
	return provider.Capabilities(), nil
}

func (r *Registry) RepositoryReader(kind Kind, host string) (RepositoryReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(RepositoryReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_repositories")
	}
	return reader, nil
}

func (r *Registry) MergeRequestReader(kind Kind, host string) (MergeRequestReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(MergeRequestReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_merge_requests")
	}
	return reader, nil
}

func (r *Registry) IssueReader(kind Kind, host string) (IssueReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(IssueReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_issues")
	}
	return reader, nil
}

func (r *Registry) ReleaseReader(kind Kind, host string) (ReleaseReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(ReleaseReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_releases")
	}
	return reader, nil
}

func (r *Registry) TagReader(kind Kind, host string) (TagReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(TagReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_tags")
	}
	return reader, nil
}

func (r *Registry) CIReader(kind Kind, host string) (CIReader, error) {
	provider, err := r.Provider(kind, host)
	if err != nil {
		return nil, err
	}

	reader, ok := provider.(CIReader)
	if !ok {
		return nil, UnsupportedCapability(kind, host, "read_ci")
	}
	return reader, nil
}
