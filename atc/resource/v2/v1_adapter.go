package v2

import (
	"context"
	"fmt"

	"github.com/concourse/concourse/atc"
	v1 "github.com/concourse/concourse/atc/resource/v1"
	"github.com/concourse/concourse/atc/worker"
)

type UnknownSpaceError struct {
	Space atc.Space
}

func (e UnknownSpaceError) Error() string {
	return fmt.Sprintf(`unknown space "%s" for v1 resource`, e.Space)
}

type V1Adapter struct {
	resource v1.Resource
}

func NewV1Adapter(container worker.Container) *V1Adapter {
	return &V1Adapter{
		resource: v1.Resource{Container: container},
	}
}

func (a *V1Adapter) Container() worker.Container {
	return a.resource.Container
}

func (a *V1Adapter) Get(
	context context.Context,
	eventHandler GetEventHandler,
	volume worker.Volume,
	ioConfig atc.IOConfig,
	source atc.Source,
	params atc.Params,
	space atc.Space,
	version atc.Version,
) error {
	_, err := a.resource.Get(context, volume, ioConfig, source, params, version)
	if err != nil {
		return err
	}

	return err
}

func (a *V1Adapter) Put(
	context context.Context,
	eventHandler PutEventHandler,
	ioConfig atc.IOConfig,
	source atc.Source,
	params atc.Params,
) ([]atc.SpaceVersion, error) {
	versionedSource, err := a.resource.Put(context, ioConfig, source, params)
	if err != nil {
		return nil, err
	}

	return []atc.SpaceVersion{
		{
			Space:    "v1space",
			Version:  versionedSource.Version(),
			Metadata: versionedSource.Metadata(),
		},
	}, nil
}

func (a *V1Adapter) Check(
	context context.Context,
	checkHandler CheckEventHandler,
	src atc.Source,
	from map[atc.Space]atc.Version,
) error {
	var version atc.Version

	if len(from) != 0 {
		var found bool
		version, found = from["v1space"]
		if !found {
			for space, _ := range from {
				return UnknownSpaceError{space}
			}
		}
	}

	versions, err := a.resource.Check(context, src, version)
	if err != nil {
		return err
	}

	err = checkHandler.DefaultSpace(atc.Space("v1space"))

	for _, v := range versions {
		err = checkHandler.Discovered(atc.Space("v1space"), v, nil)
		if err != nil {
			return err
		}
	}

	err = checkHandler.LatestVersions()

	return err
}
