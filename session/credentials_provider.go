package session

import (
	"context"
	"io"

	"github.com/gebn/bmc"
)

// Credentials represents a username and password pair, giving access to a BMC.
type Credentials struct {

	// Username is the username of the user to connect as. Only ASCII characters
	// (excluding \0) are allowed, and it cannot be more than 16 characters
	// long.
	Username string

	// Password is the password of the above user, stored on the managed system
	// as either 16 bytes (to preserve the ability to log in with a v1.5
	// session) or 20 bytes of uninterpreted data (hence why this isn't a
	// string). Passwords shorter than the maximum length are padded with 0x00.
	// This is called K_[UID] in the spec ("the key for the user with ID
	// 'UID'").
	Password []byte
}

// CredentialsRetriever is implemented by things that can find the username and
// password for a BMC. This is usually all that is necessary to establish a
// session, and is slightly simpler to implement than Provider. If you have one
// of these, you can use NewCredentialsProvider() to turn it into a Provider.
type CredentialsRetriever interface {

	// Credentials returns the username and password for the BMC at the supplied
	// address. This could be as simple as a map lookup, or it could query a
	// database or remote service.
	Credentials(ctx context.Context, addr string) (*Credentials, error)
}

// NewCredentialsProvider creates a provider from a CredentialsRetriever.
func NewCredentialsProvider(r CredentialsRetriever) Provider {
	return credentialsProvider{
		CredentialsRetriever: r,
	}
}

// credentialsProvider implements Provider using a CredentialsProvider.
type credentialsProvider struct {
	CredentialsRetriever
}

func (c credentialsProvider) Session(ctx context.Context, addr string) (bmc.Session, io.Closer, error) {
	creds, err := c.CredentialsRetriever.Credentials(ctx, addr)
	if err != nil {
		return nil, nil, err
	}
	machine, err := bmc.DialV2(addr) // TODO change to .Dial when v1.5 supported
	if err != nil {
		return nil, nil, err
	}
	sess, err := machine.NewSession(ctx, creds.Username, creds.Password)
	if err != nil {
		machine.Close()
		return nil, nil, err
	}
	return sess, machine, nil
}

// TODO how do structs implement CredentialsProvider, while having a New()
// method that returns the struct type and implements Provider? If the struct
// had a Close(), the concrete type must be returned as it is not part of the
// Provider interface. It involves embedding a Provider and the struct
// referencing itself, which is messy.
