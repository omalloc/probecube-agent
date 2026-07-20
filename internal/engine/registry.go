package engine

var defaultRegistry = NewRegistry()

type Registry struct {
	registry map[Protocol]Prober
}

func NewRegistry() *Registry {
	return &Registry{
		registry: make(map[Protocol]Prober),
	}
}

func (r *Registry) Register(proto Protocol, prober Prober) {
	r.registry[proto] = prober
}

func (r *Registry) Match(proto Protocol) (Prober, bool) {
	prober, ok := r.registry[proto]
	return prober, ok
}

func (r *Registry) AllProtocols() []Protocol {
	protos := make([]Protocol, 0, len(r.registry))
	for proto := range r.registry {
		protos = append(protos, proto)
	}
	return protos
}

func Register(proto Protocol, prober Prober) {
	defaultRegistry.Register(proto, prober)
}

func MatchProber(proto Protocol) (Prober, bool) {
	return defaultRegistry.Match(proto)
}

func AllProtocols() []Protocol {
	return defaultRegistry.AllProtocols()
}
