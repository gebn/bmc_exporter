package target

// Provider creates target structs. This exists to shield the mapper from the
// relative complexity of creating collectors and targets from them.
type Provider interface {

	// TargetFor returns a target struct for the provided address.
	TargetFor(addr string) *Target
}

// ProviderFunc allows creating a Provider implementation from a function.
type ProviderFunc func(addr string) *Target

// TargetFor implements Provider.
func (f ProviderFunc) TargetFor(addr string) *Target {
	return f(addr)
}
