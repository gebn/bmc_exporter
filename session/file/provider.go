// Package file implements a session provider that reads credentials from a
// local config file.
package file

// TODO implement hot-reloading of the config file; it may be neater to do this
// at a higher-level, e.g. a provider indicates to the exporter that it needs to
// be reloaded, which can then replace the entire web server, rather than
// locking on a map.

import (
	"context"
	"os"

	"github.com/gebn/bmc_exporter/session"

	"gopkg.in/yaml.v2"
)

// Credentials represents the username and password for a single target in a
// config file. N.B. this is not a generic credentials type; it is specific to
// this particular provider's config format.
type Credentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Provider struct {
	session.Provider

	credentials map[string]session.Credentials
}

func (p Provider) Credentials(ctx context.Context, addr string) (*session.Credentials, error) {
	creds, ok := p.credentials[addr]
	if !ok {
		return nil, session.ErrCredentialNotFound
	}
	return &creds, nil
}

func New(path string) (*Provider, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := yaml.NewDecoder(f)
	d.SetStrict(true)
	m := map[string]Credentials{}
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	// copying the map is unsatisfying, but the safest way; this code is not in
	// the hot path
	creds := make(map[string]session.Credentials, len(m))
	for addr, cred := range m {
		creds[addr] = session.Credentials{
			Username: cred.Username,
			Password: []byte(cred.Password),
		}
	}
	p := &Provider{
		credentials: creds,
	}
	p.Provider = session.NewCredentialsProvider(p)
	return p, nil
}
