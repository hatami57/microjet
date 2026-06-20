package aws

import (
	"github.com/hatami57/microjet/host"
)

// Module installs an AWS client as a managed service. The host loads its config
// (via the client's Configurable implementation) and initializes the requested
// service clients during the init phase:
//
//	host.MustNew().
//	    WithModule(aws.Module(aws.S3, aws.SQS)).
//	    MustRun()
//
// Reach the client from a Setup hook or handler with aws.Of(app).
func Module(services ...Service) host.Module {
	return NamedModule("", services...)
}

// NamedModule installs an AWS client under name, so several clients can coexist
// (retrieved with aws.Of(app, name)). Because Module already uses a variadic
// service list, naming gets its own constructor.
func NamedModule(name string, services ...Service) host.Module {
	key := "aws.Client"
	if name != "" {
		key += "[" + name + "]"
	}
	return host.KeyedModuleFunc(key, func(app *host.App) error {
		host.ProvideService(app, NewAWS(app.Logger, services...), name)
		return nil
	})
}

// Of returns the AWS client installed by Module/NamedModule under the optional
// name, panicking if none was installed.
func Of(app *host.App, name ...string) *AWS {
	return host.MustResolveService[*AWS](app, name...)
}
