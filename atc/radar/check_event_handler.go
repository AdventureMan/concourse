package radar

import (
	"fmt"

	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/db"
)

func NewCheckEventHandler(logger lager.Logger, tx db.Tx, resourceConfig db.ResourceConfig, spaces map[atc.Space]atc.Version) *CheckEventHandler {
	return &CheckEventHandler{
		logger:         logger,
		tx:             tx,
		resourceConfig: resourceConfig,
		spaces:         spaces,
	}
}

type CheckEventHandler struct {
	logger         lager.Logger
	tx             db.Tx
	resourceConfig db.ResourceConfig
	spaces         map[atc.Space]atc.Version
}

func (c *CheckEventHandler) SaveDefault(space atc.Space) error {
	if space != "" {
		err := c.resourceConfig.SaveDefaultSpace(space)
		if err != nil {
			c.logger.Error("failed-to-save-default-space", err, lager.Data{
				"space": space,
			})
			return err
		}

		c.logger.Debug("default-space-saved", lager.Data{
			"space": space,
		})
	}

	return nil
}

func (c *CheckEventHandler) Save(space atc.Space, version atc.Version, metadata atc.Metadata) error {
	if _, ok := c.spaces[space]; !ok {
		err := c.resourceConfig.SaveSpace(space)
		if err != nil {
			c.logger.Error("failed-to-save-space", err, lager.Data{
				"space": space,
			})
			return err
		}

		c.logger.Debug("space-saved", lager.Data{
			"space": space,
		})
	}

	err := c.resourceConfig.SavePartialVersion(space, version, metadata)
	if err != nil {
		c.logger.Error("failed-to-save-resource-config-version", err, lager.Data{
			"version": fmt.Sprintf("%v", version),
		})
		return err
	}

	c.logger.Debug("version-saved", lager.Data{
		"space":   space,
		"version": fmt.Sprintf("%v", version),
	})

	c.spaces[space] = version
	return nil
}

func (c *CheckEventHandler) Finish() error {
	if len(c.spaces) == 0 {
		c.logger.Debug("no-new-versions")
		return nil
	}

	err := c.resourceConfig.FinishSavingVersions()
	if err != nil {
		return err
	}

	updated, err := c.resourceConfig.UpdateLastCheckFinished()
	if err != nil {
		return err
	}

	if !updated {
		c.logger.Debug("did-not-update-last-check-finished")
	}

	for space, version := range c.spaces {
		err := c.resourceConfig.SaveSpaceLatestVersion(space, version)
		if err != nil {
			return err
		}
	}

	return nil
}
