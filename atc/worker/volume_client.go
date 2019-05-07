package worker

import (
	"errors"
	"fmt"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/lager"
	"github.com/concourse/baggageclaim"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/db/lock"
	"github.com/concourse/concourse/atc/metric"
)

const creatingVolumeRetryDelay = 1 * time.Second

//go:generate counterfeiter . VolumeClient

type VolumeClient interface {
	FindOrCreateVolumeForContainer(
		lager.Logger,
		VolumeSpec,
		db.CreatingContainer,
		int,
		string,
	) (Artifact, error)
	FindOrCreateCOWVolumeForContainer(
		lager.Logger,
		VolumeSpec,
		db.CreatingContainer,
		Artifact,
		int,
		string,
	) (Artifact, error)
	FindOrCreateVolumeForBaseResourceType(
		lager.Logger,
		VolumeSpec,
		int,
		string,
	) (Artifact, error)
	FindOrCreateVolumeForArtifact(
		lager.Logger,
		VolumeSpec,
		int,
		db.WorkerArtifact,
		string,
		db.VolumeType,
	) (Artifact, error)
	FindVolumeForResourceCache(
		lager.Logger,
		db.UsedResourceCache,
	) (Artifact, bool, error)
	FindVolumeForTaskCache(
		logger lager.Logger,
		teamID int,
		jobID int,
		stepName string,
		path string,
	) (Artifact, bool, error)
	CreateVolumeForTaskCache(
		logger lager.Logger,
		volumeSpec VolumeSpec,
		teamID int,
		jobID int,
		stepName string,
		path string,
	) (Artifact, error)
	FindOrCreateVolumeForResourceCerts(
		logger lager.Logger,
	) (volume Artifact, found bool, err error)

	LookupVolume(lager.Logger, string) (Artifact, bool, error)
}

type VolumeSpec struct {
	Strategy   baggageclaim.Strategy
	Properties VolumeProperties
	Privileged bool
	TTL        time.Duration
}

func (spec VolumeSpec) baggageclaimVolumeSpec() baggageclaim.VolumeSpec {
	return baggageclaim.VolumeSpec{
		Strategy:   spec.Strategy,
		Privileged: spec.Privileged,
		Properties: baggageclaim.VolumeProperties(spec.Properties),
	}
}

type VolumeProperties map[string]string

type ErrCreatedVolumeNotFound struct {
	Handle     string
	WorkerName string
}

func (e ErrCreatedVolumeNotFound) Error() string {
	return fmt.Sprintf("volume '%s' disappeared from worker '%s'", e.Handle, e.WorkerName)
}

var ErrBaseResourceTypeNotFound = errors.New("base resource type not found")

type volumeClient struct {
	baggageclaimClient              baggageclaim.Client
	lockFactory                     lock.LockFactory
	dbVolumeRepository              db.VolumeRepository
	dbWorkerBaseResourceTypeFactory db.WorkerBaseResourceTypeFactory
	dbWorkerTaskCacheFactory        db.WorkerTaskCacheFactory
	clock                           clock.Clock
	dbWorker                        db.Worker
}

func NewVolumeClient(
	baggageclaimClient baggageclaim.Client,
	dbWorker db.Worker,
	clock clock.Clock,

	lockFactory lock.LockFactory,
	dbVolumeRepository db.VolumeRepository,
	dbWorkerBaseResourceTypeFactory db.WorkerBaseResourceTypeFactory,
	dbWorkerTaskCacheFactory db.WorkerTaskCacheFactory,
) VolumeClient {
	return &volumeClient{
		baggageclaimClient:              baggageclaimClient,
		lockFactory:                     lockFactory,
		dbVolumeRepository:              dbVolumeRepository,
		dbWorkerBaseResourceTypeFactory: dbWorkerBaseResourceTypeFactory,
		dbWorkerTaskCacheFactory:        dbWorkerTaskCacheFactory,
		clock:                           clock,
		dbWorker:                        dbWorker,
	}
}

func (c *volumeClient) FindOrCreateVolumeForContainer(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	container db.CreatingContainer,
	teamID int,
	mountPath string,
) (Artifact, error) {
	return c.findOrCreateVolume(
		logger.Session("find-or-create-volume-for-container"),
		volumeSpec,
		nil,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return c.dbVolumeRepository.FindContainerVolume(teamID, c.dbWorker.Name(), container, mountPath)
		},
		func() (db.CreatingVolume, error) {
			return c.dbVolumeRepository.CreateContainerVolume(teamID, c.dbWorker.Name(), container, mountPath)
		},
	)
}

func (c *volumeClient) FindOrCreateCOWVolumeForContainer(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	container db.CreatingContainer,
	parent Artifact,
	teamID int,
	mountPath string,
) (Artifact, error) {
	return c.findOrCreateVolume(
		logger.Session("find-or-create-cow-volume-for-container"),
		volumeSpec,
		nil,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return c.dbVolumeRepository.FindContainerVolume(teamID, c.dbWorker.Name(), container, mountPath)
		},
		func() (db.CreatingVolume, error) {
			return parent.CreateChildForContainer(container, mountPath)
		},
	)
}

func (c *volumeClient) FindOrCreateVolumeForArtifact(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	teamID int,
	artifact db.WorkerArtifact,
	workerName string,
	volumeType db.VolumeType,
) (Artifact, error) {
	return c.findOrCreateVolume(
		logger.Session("find-or-create-volume-for-artifact"),
		volumeSpec,
		artifact,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return c.dbVolumeRepository.FindArtifactVolume(artifact.ID())
		},
		func() (db.CreatingVolume, error) {
			return c.dbVolumeRepository.CreateVolumeForArtifact(teamID, artifact.ID(), workerName, volumeType)
		},
	)
}

func (c *volumeClient) FindOrCreateVolumeForBaseResourceType(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	teamID int,
	resourceTypeName string,
) (Artifact, error) {
	workerBaseResourceType, found, err := c.dbWorkerBaseResourceTypeFactory.Find(resourceTypeName, c.dbWorker)
	if err != nil {
		return nil, err
	}

	if !found {
		logger.Error("base-resource-type-not-found", ErrBaseResourceTypeNotFound, lager.Data{"resource-type-name": resourceTypeName})
		return nil, ErrBaseResourceTypeNotFound
	}

	return c.findOrCreateVolume(
		logger.Session("find-or-create-volume-for-base-resource-type"),
		volumeSpec,
		nil,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return c.dbVolumeRepository.FindBaseResourceTypeVolume(workerBaseResourceType)
		},
		func() (db.CreatingVolume, error) {
			return c.dbVolumeRepository.CreateBaseResourceTypeVolume(workerBaseResourceType)
		},
	)
}

func (c *volumeClient) FindVolumeForResourceCache(
	logger lager.Logger,
	usedResourceCache db.UsedResourceCache,
) (Artifact, bool, error) {
	dbVolume, found, err := c.dbVolumeRepository.FindResourceCacheVolume(c.dbWorker.Name(), usedResourceCache)
	if err != nil {
		logger.Error("failed-to-lookup-resource-cache-volume-in-db", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	bcVolume, found, err := c.baggageclaimClient.LookupVolume(logger, dbVolume.Handle())
	if err != nil {
		logger.Error("failed-to-lookup-volume-in-bc", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	return NewArtifactForVolume(bcVolume, dbVolume, c), true, nil
}

func (c *volumeClient) CreateVolumeForTaskCache(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	teamID int,
	jobID int,
	stepName string,
	path string,
) (Artifact, error) {
	taskCache, err := c.dbWorkerTaskCacheFactory.FindOrCreate(jobID, stepName, path, c.dbWorker.Name())
	if err != nil {
		logger.Error("failed-to-find-or-create-task-cache-in-db", err)
		return nil, err
	}

	return c.findOrCreateVolume(
		logger.Session("find-or-create-volume-for-container"),
		volumeSpec,
		nil,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return nil, nil, nil
		},
		func() (db.CreatingVolume, error) {
			return c.dbVolumeRepository.CreateTaskCacheVolume(teamID, taskCache)
		},
	)
}

func (c *volumeClient) FindOrCreateVolumeForResourceCerts(logger lager.Logger) (Artifact, bool, error) {

	logger.Debug("finding-worker-resource-certs")
	usedResourceCerts, found, err := c.dbWorker.ResourceCerts()
	if err != nil {
		logger.Error("failed-to-find-worker-resource-certs", err)
		return nil, false, err
	}

	if !found {
		logger.Debug("worker-resource-certs-not-found")
		return nil, false, nil
	}

	certsPath := c.dbWorker.CertsPath()
	if certsPath == nil {
		logger.Debug("worker-certs-path-is-empty")
		return nil, false, nil
	}

	volume, err := c.findOrCreateVolume(
		logger.Session("find-or-create-volume-for-resource-certs"),
		VolumeSpec{
			Strategy: baggageclaim.ImportStrategy{
				Path:           *certsPath,
				FollowSymlinks: true,
			},
		},
		nil,
		func() (db.CreatingVolume, db.CreatedVolume, error) {
			return c.dbVolumeRepository.FindResourceCertsVolume(c.dbWorker.Name(), usedResourceCerts)
		},
		func() (db.CreatingVolume, error) {
			return c.dbVolumeRepository.CreateResourceCertsVolume(c.dbWorker.Name(), usedResourceCerts)
		},
	)

	return volume, true, err
}

func (c *volumeClient) FindVolumeForTaskCache(
	logger lager.Logger,
	teamID int,
	jobID int,
	stepName string,
	path string,
) (Artifact, bool, error) {
	taskCache, found, err := c.dbWorkerTaskCacheFactory.Find(jobID, stepName, path, c.dbWorker.Name())
	if err != nil {
		logger.Error("failed-to-lookup-task-cache-in-db", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	_, dbVolume, err := c.dbVolumeRepository.FindTaskCacheVolume(teamID, taskCache)
	if err != nil {
		logger.Error("failed-to-lookup-tasl-cache-volume-in-db", err)
		return nil, false, err
	}

	if dbVolume == nil {
		return nil, false, nil
	}

	bcVolume, found, err := c.baggageclaimClient.LookupVolume(logger, dbVolume.Handle())
	if err != nil {
		logger.Error("failed-to-lookup-volume-in-bc", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	return NewArtifactForVolume(bcVolume, dbVolume, c), true, nil
}

func (c *volumeClient) LookupVolume(logger lager.Logger, handle string) (Artifact, bool, error) {
	dbVolume, found, err := c.dbVolumeRepository.FindCreatedVolume(handle)
	if err != nil {
		logger.Error("failed-to-lookup-volume-in-db", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	bcVolume, found, err := c.baggageclaimClient.LookupVolume(logger, handle)
	if err != nil {
		logger.Error("failed-to-lookup-volume-in-bc", err)
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	return NewArtifactForVolume(bcVolume, dbVolume, c), true, nil
}

func (c *volumeClient) findOrCreateVolume(
	logger lager.Logger,
	volumeSpec VolumeSpec,
	dbArtifact db.WorkerArtifact,
	findVolumeFunc func() (db.CreatingVolume, db.CreatedVolume, error),
	createVolumeFunc func() (db.CreatingVolume, error),
) (Artifact, error) {
	creatingVolume, createdVolume, err := findVolumeFunc()
	if err != nil {
		logger.Error("failed-to-find-volume-in-db", err)
		return nil, err
	}

	if createdVolume != nil {
		logger = logger.WithData(lager.Data{"volume": createdVolume.Handle()})

		bcVolume, bcVolumeFound, err := c.baggageclaimClient.LookupVolume(
			logger.Session("lookup-volume"),
			createdVolume.Handle(),
		)
		if err != nil {
			logger.Error("failed-to-lookup-volume-in-baggageclaim", err)
			return nil, err
		}

		if !bcVolumeFound {
			logger.Info("created-volume-not-found")
			return nil, ErrCreatedVolumeNotFound{Handle: createdVolume.Handle(), WorkerName: createdVolume.WorkerName()}
		}

		logger.Debug("found-created-volume")

		return NewArtifactForVolume(bcVolume, createdVolume, c), nil
	}

	if creatingVolume != nil {
		logger = logger.WithData(lager.Data{"volume": creatingVolume.Handle()})
		logger.Debug("found-creating-volume")
	} else {
		creatingVolume, err = createVolumeFunc()
		if err != nil {
			logger.Error("failed-to-create-volume-in-db", err)
			return nil, err
		}

		logger = logger.WithData(lager.Data{"volume": creatingVolume.Handle()})

		logger.Debug("created-creating-volume")
	}

	lock, acquired, err := c.lockFactory.Acquire(logger, lock.NewVolumeCreatingLockID(creatingVolume.ID()))
	if err != nil {
		logger.Error("failed-to-acquire-volume-creating-lock", err)
		return nil, err
	}

	if !acquired {
		c.clock.Sleep(creatingVolumeRetryDelay)
		return c.findOrCreateVolume(logger, volumeSpec, dbArtifact, findVolumeFunc, createVolumeFunc)
	}

	defer lock.Release()

	bcVolume, bcVolumeFound, err := c.baggageclaimClient.LookupVolume(
		logger.Session("create-volume"),
		creatingVolume.Handle(),
	)
	if err != nil {
		logger.Error("failed-to-lookup-volume-in-baggageclaim", err)
		return nil, err
	}

	if bcVolumeFound {
		logger.Debug("real-volume-exists")
	} else {
		logger.Debug("creating-real-volume")

		bcVolume, err = c.baggageclaimClient.CreateVolume(
			logger.Session("create-volume"),
			creatingVolume.Handle(),
			volumeSpec.baggageclaimVolumeSpec(),
		)
		if err != nil {
			logger.Error("failed-to-create-volume-in-baggageclaim", err)

			_, failedErr := creatingVolume.Failed()
			if failedErr != nil {
				logger.Error("failed-to-mark-volume-as-failed", failedErr)
			}

			metric.FailedVolumes.Inc()

			return nil, err
		}

		metric.VolumesCreated.Inc()
	}

	createdVolume, err = creatingVolume.Created()
	if err != nil {
		logger.Error("failed-to-initialize-volume", err)
		return nil, err
	}

	logger.Debug("created")

	return NewArtifactForVolume(dbArtifact, bcVolume, createdVolume, c), nil
}
