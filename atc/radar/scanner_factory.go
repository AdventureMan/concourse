package radar

import (
	"time"

	"code.cloudfoundry.org/clock"
	"github.com/concourse/concourse/atc/creds"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/resource"
)

// ScannerFactory is the same interface as resourceserver/server.go
// They are in two places because there would be cyclic dependencies otherwise

// go:generate counterfeiter . ScannerFactory
type ScannerFactory interface {
	NewResourceScanner(dbPipeline db.Pipeline) Scanner
	NewResourceTypeScanner(dbPipeline db.Pipeline) Scanner
}

type scannerFactory struct {
	resourceFactory              resource.ResourceFactory
	resourceTypeCheckingInterval time.Duration
	resourceCheckingInterval     time.Duration
	externalURL                  string
	variablesFactory             creds.VariablesFactory

	conn db.Conn
}

var ContainerExpiries = db.ContainerOwnerExpiries{
	GraceTime: 2 * time.Minute,
	Min:       5 * time.Minute,
	Max:       1 * time.Hour,
}

func NewScannerFactory(
	conn db.Conn,
	resourceFactory resource.ResourceFactory,
	resourceTypeCheckingInterval time.Duration,
	resourceCheckingInterval time.Duration,
	externalURL string,
	variablesFactory creds.VariablesFactory,
) ScannerFactory {
	return &scannerFactory{
		conn:                         conn,
		resourceFactory:              resourceFactory,
		resourceCheckingInterval:     resourceCheckingInterval,
		resourceTypeCheckingInterval: resourceTypeCheckingInterval,
		externalURL:                  externalURL,
		variablesFactory:             variablesFactory,
	}
}

func (f *scannerFactory) NewResourceScanner(dbPipeline db.Pipeline) Scanner {
	variables := f.variablesFactory.NewVariables(dbPipeline.TeamName(), dbPipeline.Name())

	return NewResourceScanner(
		f.conn,
		clock.NewClock(),
		f.resourceFactory,
		f.resourceCheckingInterval,
		dbPipeline,
		f.externalURL,
		variables,
	)
}

func (f *scannerFactory) NewResourceTypeScanner(dbPipeline db.Pipeline) Scanner {
	variables := f.variablesFactory.NewVariables(dbPipeline.TeamName(), dbPipeline.Name())

	return NewResourceTypeScanner(
		f.conn,
		clock.NewClock(),
		f.resourceFactory,
		f.resourceTypeCheckingInterval,
		dbPipeline,
		f.externalURL,
		variables,
	)
}
