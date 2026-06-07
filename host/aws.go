package host

import (
	"github.com/hatami57/microjet/aws"
)

// WithAWS registers the AWS client as a service. The host's initServices
// loads config via AWS's Configurable implementation and then initializes
// the requested service clients via its Init method.
func (a *App) WithAWS(services ...aws.Service) *App {
	if a.err != nil {
		return a
	}
	client := aws.NewAWS(a.Logger, services...)
	a.AWS = client
	ProvideService(a, client)
	return a
}
