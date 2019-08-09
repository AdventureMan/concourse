package engine

import (
	"context"
	"sync"
	"time"

	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagerctx"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/exec"
	"github.com/concourse/concourse/atc/metric"
)

//go:generate counterfeiter . Engine

type Engine interface {
	NewBuild(db.Build) Runnable
	ReleaseAll(lager.Logger)
}

//go:generate counterfeiter . Runnable

type Runnable interface {
	Run(logger lager.Logger)
}

//go:generate counterfeiter . StepBuilder

type StepBuilder interface {
	BuildStep(db.Build) (exec.Step, error)
}

func NewEngine(builder StepBuilder) Engine {
	return &engine{
		builder: builder,

		release:       make(chan bool),
		trackedStates: new(sync.Map),
		waitGroup:     new(sync.WaitGroup),
	}
}

type engine struct {
	builder StepBuilder

	release       chan bool
	trackedStates *sync.Map
	waitGroup     *sync.WaitGroup
}

func (engine *engine) ReleaseAll(logger lager.Logger) {
	logger.Info("calling-release-on-builds")

	close(engine.release)

	logger.Info("waiting-on-builds")

	engine.waitGroup.Wait()

	logger.Info("finished-waiting-on-builds")
}

func (engine *engine) NewBuild(build db.Build) Runnable {

	ctx, cancel := context.WithCancel(context.Background())

	return NewBuild(
		ctx,
		cancel,
		build,
		engine.builder,
		engine.release,
		engine.trackedStates,
		engine.waitGroup,
	)
}

func NewBuild(
	ctx context.Context,
	cancel func(),
	build db.Build,
	builder StepBuilder,
	release chan bool,
	trackedStates *sync.Map,
	waitGroup *sync.WaitGroup,
) Runnable {
	return &engineBuild{
		ctx:    ctx,
		cancel: cancel,

		build:   build,
		builder: builder,

		release:       release,
		trackedStates: trackedStates,
		waitGroup:     waitGroup,
	}
}

type engineBuild struct {
	ctx    context.Context
	cancel func()

	build   db.Build
	builder StepBuilder

	release       chan bool
	trackedStates *sync.Map
	waitGroup     *sync.WaitGroup
}

func (b *engineBuild) Run(logger lager.Logger) {
	b.waitGroup.Add(1)
	defer b.waitGroup.Done()

	logger = logger.WithData(lager.Data{
		"build":    b.build.ID(),
		"pipeline": b.build.PipelineName(),
		"job":      b.build.JobName(),
	})

	lock, acquired, err := b.build.AcquireTrackingLock(logger, time.Minute)
	if err != nil {
		logger.Error("failed-to-get-lock", err)
		return
	}

	if !acquired {
		logger.Debug("build-already-tracked")
		return
	}

	defer lock.Release()

	found, err := b.build.Reload()
	if err != nil {
		logger.Error("failed-to-load-build-from-db", err)
		return
	}

	if !found {
		logger.Info("build-not-found")
		return
	}

	if !b.build.IsRunning() {
		logger.Info("build-already-finished")
		return
	}

	notifier, err := b.build.AbortNotifier()
	if err != nil {
		logger.Error("failed-to-listen-for-aborts", err)
		return
	}

	defer notifier.Close()

	step, err := b.builder.BuildStep(b.build)
	if err != nil {
		logger.Error("failed-to-build-step", err)
		return
	}

	b.trackStarted()
	defer b.trackFinished(logger)

	logger.Info("running")

	state := b.runState()
	defer b.clearRunState()

	noleak := make(chan bool)
	defer close(noleak)

	go func() {
		select {
		case <-noleak:
		case <-notifier.Notify():
			logger.Info("aborting")
			b.cancel()
		}
	}()

	done := make(chan error)
	go func() {
		ctx := lagerctx.NewContext(b.ctx, logger)
		done <- step.Run(ctx, state)
	}()

	select {
	case <-b.release:
		logger.Info("releasing")

	case err = <-done:
		b.finish(logger.Session("finish"), err, step.Succeeded())
	}
}

func (b *engineBuild) finish(logger lager.Logger, err error, succeeded bool) {
	if err == context.Canceled {
		b.saveStatus(logger, atc.StatusAborted)
		logger.Info("aborted")

	} else if err != nil {
		b.saveStatus(logger, atc.StatusErrored)
		logger.Info("errored", lager.Data{"error": err.Error()})

	} else if succeeded {
		b.saveStatus(logger, atc.StatusSucceeded)
		logger.Info("succeeded")

	} else {
		b.saveStatus(logger, atc.StatusFailed)
		logger.Info("failed")
	}
}

func (b *engineBuild) saveStatus(logger lager.Logger, status atc.BuildStatus) {
	if err := b.build.Finish(db.BuildStatus(status)); err != nil {
		logger.Error("failed-to-finish-build", err)
	}
}

func (b *engineBuild) trackStarted() {
	metric.BuildsStarted.Inc()
}

func (b *engineBuild) trackFinished(logger lager.Logger) {
	found, err := b.build.Reload()
	if err != nil {
		logger.Error("failed-to-load-build-from-db", err)
		return
	}

	if !found {
		logger.Info("build-removed")
		return
	}

	if !b.build.IsRunning() {
		metric.
			BuildsDuration.
			WithLabelValues(string(b.build.Status())).
			Observe(b.build.EndTime().Sub(b.build.StartTime()).Seconds())
	}
}

func (build *engineBuild) runState() exec.RunState {
	existingState, _ := build.trackedStates.LoadOrStore(build.build.ID(), exec.NewRunState())
	return existingState.(exec.RunState)
}

func (build *engineBuild) clearRunState() {
	build.trackedStates.Delete(build.build.ID())
}
