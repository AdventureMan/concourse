package exec_test

import (
	"context"
	"errors"

	"github.com/cloudfoundry/bosh-cli/director/template"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/creds"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/db/dbfakes"
	"github.com/concourse/concourse/atc/exec"
	"github.com/concourse/concourse/atc/exec/execfakes"
	"github.com/concourse/concourse/atc/resource/resourcefakes"
	"github.com/concourse/concourse/atc/worker"
	"github.com/concourse/concourse/atc/worker/workerfakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("PutStep", func() {
	var (
		ctx    context.Context
		cancel func()

		fakeBuild *dbfakes.FakeBuild

		pipelineResourceName string

		fakeResourceFactory       *resourcefakes.FakeResourceFactory
		fakeResourceConfigFactory *dbfakes.FakeResourceConfigFactory

		variables creds.Variables

		stepMetadata testMetadata = []string{"a=1", "b=2"}

		containerMetadata = db.ContainerMetadata{
			Type:     db.ContainerTypePut,
			StepName: "some-step",
		}
		planID       atc.PlanID
		fakeDelegate *execfakes.FakePutDelegate

		resourceTypes creds.VersionedResourceTypes

		repo  *worker.ArtifactRepository
		state *execfakes.FakeRunState

		stdoutBuf *gbytes.Buffer
		stderrBuf *gbytes.Buffer

		putStep *exec.PutStep
		stepErr error
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		fakeBuild = new(dbfakes.FakeBuild)
		fakeBuild.IDReturns(42)
		fakeBuild.TeamIDReturns(123)

		planID = atc.PlanID("some-plan-id")

		pipelineResourceName = "some-resource"

		fakeResourceFactory = new(resourcefakes.FakeResourceFactory)
		fakeResourceConfigFactory = new(dbfakes.FakeResourceConfigFactory)
		variables = template.StaticVariables{
			"custom-param": "source",
			"source-param": "super-secret-source",
		}

		fakeDelegate = new(execfakes.FakePutDelegate)
		stdoutBuf = gbytes.NewBuffer()
		stderrBuf = gbytes.NewBuffer()
		fakeDelegate.StdoutReturns(stdoutBuf)
		fakeDelegate.StderrReturns(stderrBuf)

		repo = worker.NewArtifactRepository()
		state = new(execfakes.FakeRunState)
		state.ArtifactsReturns(repo)

		resourceTypes = creds.NewVersionedResourceTypes(variables, atc.VersionedResourceTypes{
			{
				ResourceType: atc.ResourceType{
					Name:   "custom-resource",
					Type:   "custom-type",
					Source: atc.Source{"some-custom": "((custom-param))"},
				},
				Version: atc.Version{"some-custom": "version"},
			},
		})

		stepErr = nil
	})

	AfterEach(func() {
		cancel()
	})

	JustBeforeEach(func() {
		putStep = exec.NewPutStep(
			fakeBuild,
			"some-name",
			"some-resource-type",
			pipelineResourceName,
			creds.NewSource(variables, atc.Source{"some": "((source-param))"}),
			creds.NewParams(variables, atc.Params{"some-param": "some-value"}),
			[]string{"some", "tags"},
			fakeDelegate,
			fakeResourceFactory,
			fakeResourceConfigFactory,
			planID,
			containerMetadata,
			stepMetadata,
			resourceTypes,
		)

		stepErr = putStep.Run(ctx, state)
	})

	Context("when repo contains sources", func() {
		var (
			fakeSource        *workerfakes.FakeArtifactSource
			fakeOtherSource   *workerfakes.FakeArtifactSource
			fakeMountedSource *workerfakes.FakeArtifactSource
		)

		BeforeEach(func() {
			fakeSource = new(workerfakes.FakeArtifactSource)
			fakeOtherSource = new(workerfakes.FakeArtifactSource)
			fakeMountedSource = new(workerfakes.FakeArtifactSource)

			repo.RegisterSource("some-source", fakeSource)
			repo.RegisterSource("some-other-source", fakeOtherSource)
			repo.RegisterSource("some-mounted-source", fakeMountedSource)
		})

		Context("when the tracker can initialize the resource", func() {
			var (
				fakeResource       *resourcefakes.FakeResource
				fakeResourceConfig *dbfakes.FakeResourceConfig
				versions           []atc.SpaceVersion
			)

			BeforeEach(func() {
				fakeResource = new(resourcefakes.FakeResource)
				fakeResourceFactory.NewResourceReturns(fakeResource, nil)

				fakeResourceConfig = new(dbfakes.FakeResourceConfig)
				fakeResourceConfig.IDReturns(1)

				fakeResourceConfigFactory.FindOrCreateResourceConfigReturns(fakeResourceConfig, nil)

				versions = []atc.SpaceVersion{
					{
						Space:   atc.Space("space"),
						Version: atc.Version{"some": "version"},
						Metadata: atc.Metadata{
							atc.MetadataField{
								Name:  "some",
								Value: "metadata",
							},
						},
					},
				}

				fakeResource.PutReturns(versions, nil)
			})

			It("finds or creates a resource config", func() {
				Expect(fakeResourceConfigFactory.FindOrCreateResourceConfigCallCount()).To(Equal(1))
				_, actualResourceType, actualSource, actualResourceTypes := fakeResourceConfigFactory.FindOrCreateResourceConfigArgsForCall(0)
				Expect(actualResourceType).To(Equal("some-resource-type"))
				Expect(actualSource).To(Equal(atc.Source{"some": "super-secret-source"}))
				Expect(actualResourceTypes).To(Equal(resourceTypes))
			})

			It("initializes the resource with the correct type, session, and sources", func() {
				Expect(fakeResourceFactory.NewResourceCallCount()).To(Equal(1))

				_, _, owner, cm, containerSpec, actualResourceTypes, delegate, resourceConfig := fakeResourceFactory.NewResourceArgsForCall(0)
				Expect(cm).To(Equal(containerMetadata))
				Expect(owner).To(Equal(db.NewBuildStepContainerOwner(42, atc.PlanID(planID), 123)))
				Expect(containerSpec.ImageSpec).To(Equal(worker.ImageSpec{
					ResourceType: "some-resource-type",
				}))
				Expect(containerSpec.Tags).To(Equal([]string{"some", "tags"}))
				Expect(containerSpec.TeamID).To(Equal(123))
				Expect(containerSpec.Env).To(Equal([]string{"a=1", "b=2"}))
				Expect(containerSpec.Dir).To(Equal("/tmp/build/put"))
				Expect(containerSpec.Inputs).To(HaveLen(3))
				Expect([]worker.ArtifactSource{
					containerSpec.Inputs[0].Source(),
					containerSpec.Inputs[1].Source(),
					containerSpec.Inputs[2].Source(),
				}).To(ConsistOf(
					exec.PutResourceSource{fakeSource},
					exec.PutResourceSource{fakeOtherSource},
					exec.PutResourceSource{fakeMountedSource},
				))
				Expect(actualResourceTypes).To(Equal(resourceTypes))
				Expect(delegate).To(Equal(fakeDelegate))
				Expect(resourceConfig.ID()).To(Equal(1))
			})

			It("puts the resource with the given context", func() {
				Expect(fakeResource.PutCallCount()).To(Equal(1))
				putCtx, _, _, _, _ := fakeResource.PutArgsForCall(0)
				Expect(putCtx).To(Equal(ctx))
			})

			It("puts the resource with the correct source and params", func() {
				Expect(fakeResource.PutCallCount()).To(Equal(1))

				_, _, _, putSource, putParams := fakeResource.PutArgsForCall(0)
				Expect(putSource).To(Equal(atc.Source{"some": "super-secret-source"}))
				Expect(putParams).To(Equal(atc.Params{"some-param": "some-value"}))
			})

			It("puts the resource with the io config forwarded", func() {
				Expect(fakeResource.PutCallCount()).To(Equal(1))

				_, _, ioConfig, _, _ := fakeResource.PutArgsForCall(0)
				Expect(ioConfig.Stdout).To(Equal(stdoutBuf))
				Expect(ioConfig.Stderr).To(Equal(stderrBuf))
			})

			It("runs the get resource action", func() {
				Expect(fakeResource.PutCallCount()).To(Equal(1))
			})

			Context("when a version is returned", func() {
				It("reports the created version info", func() {
					info := putStep.VersionInfo()
					Expect(info.Space).To(Equal(atc.Space("space")))
					Expect(info.Version).To(Equal(atc.Version{"some": "version"}))
					Expect(info.Metadata).To(Equal([]atc.MetadataField{{"some", "metadata"}}))
				})
			})

			Context("when no versions are returned", func() {
				BeforeEach(func() {
					versions = []atc.SpaceVersion{}
					fakeResource.PutReturns(versions, nil)
				})

				It("reports empty created version info", func() {
					info := putStep.VersionInfo()
					Expect(info.Space).To(Equal(atc.Space("")))
					Expect(info.Version).To(BeNil())
					Expect(info.Metadata).To(BeNil())
				})
			})

			Context("when multiple versions are returned", func() {
				BeforeEach(func() {
					versions = []atc.SpaceVersion{
						{
							Space:   atc.Space("space"),
							Version: atc.Version{"some": "version"},
							Metadata: atc.Metadata{
								atc.MetadataField{
									Name:  "some",
									Value: "metadata",
								},
							},
						},
						{
							Space:   atc.Space("space"),
							Version: atc.Version{"some": "other-version"},
							Metadata: atc.Metadata{
								atc.MetadataField{
									Name:  "some",
									Value: "other-metadata",
								},
							},
						},
					}

					fakeResource.PutReturns(versions, nil)
				})

				It("reports the latest version returned from the put step into the version info", func() {
					info := putStep.VersionInfo()
					Expect(info.Space).To(Equal(atc.Space("space")))
					Expect(info.Version).To(Equal(atc.Version{"some": "other-version"}))
					Expect(info.Metadata).To(Equal([]atc.MetadataField{{"some", "other-metadata"}}))
				})
			})

			It("is successful", func() {
				Expect(putStep.Succeeded()).To(BeTrue())
			})

			Context("when a version is returned by the put", func() {
				It("saves the build output", func() {
					Expect(fakeBuild.SaveOutputCallCount()).To(Equal(1))

					resourceConfig, spaceVersion, outputName, resourceName := fakeBuild.SaveOutputArgsForCall(0)
					Expect(resourceConfig.ID()).To(Equal(fakeResourceConfig.ID()))
					Expect(spaceVersion).To(Equal(atc.SpaceVersion{
						Space:   atc.Space("space"),
						Version: atc.Version{"some": "version"},
						Metadata: atc.Metadata{
							atc.MetadataField{
								Name:  "some",
								Value: "metadata",
							},
						},
					}))
					Expect(outputName).To(Equal("some-name"))
					Expect(resourceName).To(Equal("some-resource"))
				})
			})

			Context("when multiple versions are return by the put", func() {
				BeforeEach(func() {
					versions = []atc.SpaceVersion{
						{
							Space:   atc.Space("space"),
							Version: atc.Version{"some": "version"},
							Metadata: atc.Metadata{
								atc.MetadataField{
									Name:  "some",
									Value: "metadata",
								},
							},
						},
						{
							Space:   atc.Space("space"),
							Version: atc.Version{"some": "other-version"},
							Metadata: atc.Metadata{
								atc.MetadataField{
									Name:  "some",
									Value: "other-metadata",
								},
							},
						},
					}

					fakeResource.PutReturns(versions, nil)
				})

				It("saves the build output for all versions", func() {
					Expect(fakeBuild.SaveOutputCallCount()).To(Equal(2))

					resourceConfig, spaceVersion, outputName, resourceName := fakeBuild.SaveOutputArgsForCall(0)
					Expect(resourceConfig.ID()).To(Equal(fakeResourceConfig.ID()))
					Expect(spaceVersion).To(Equal(atc.SpaceVersion{
						Space:   atc.Space("space"),
						Version: atc.Version{"some": "version"},
						Metadata: atc.Metadata{
							atc.MetadataField{
								Name:  "some",
								Value: "metadata",
							},
						},
					}))
					Expect(outputName).To(Equal("some-name"))
					Expect(resourceName).To(Equal("some-resource"))

					resourceConfig, spaceVersion, outputName, resourceName = fakeBuild.SaveOutputArgsForCall(1)
					Expect(resourceConfig.ID()).To(Equal(fakeResourceConfig.ID()))
					Expect(spaceVersion).To(Equal(atc.SpaceVersion{
						Space:   atc.Space("space"),
						Version: atc.Version{"some": "other-version"},
						Metadata: atc.Metadata{
							atc.MetadataField{
								Name:  "some",
								Value: "other-metadata",
							},
						},
					}))
					Expect(outputName).To(Equal("some-name"))
					Expect(resourceName).To(Equal("some-resource"))
				})
			})

			Context("when the resource is blank", func() {
				BeforeEach(func() {
					pipelineResourceName = ""
				})

				It("is successful", func() {
					Expect(putStep.Succeeded()).To(BeTrue())
				})

				It("does not save the build output", func() {
					Expect(fakeBuild.SaveOutputCallCount()).To(Equal(0))
				})
			})

			It("finishes via the delegate", func() {
				Expect(fakeDelegate.FinishedCallCount()).To(Equal(1))
				_, status, info := fakeDelegate.FinishedArgsForCall(0)
				Expect(status).To(Equal(exec.ExitStatus(0)))
				Expect(info.Space).To(Equal(atc.Space("space")))
				Expect(info.Version).To(Equal(atc.Version{"some": "version"}))
				Expect(info.Metadata).To(Equal([]atc.MetadataField{{"some", "metadata"}}))
			})

			It("stores the version info as the step result", func() {
				Expect(state.StoreResultCallCount()).To(Equal(1))
				sID, sVal := state.StoreResultArgsForCall(0)
				Expect(sID).To(Equal(planID))
				Expect(sVal).To(Equal(exec.VersionInfo{
					Space:    atc.Space("space"),
					Version:  atc.Version{"some": "version"},
					Metadata: []atc.MetadataField{{"some", "metadata"}},
				}))
			})

			Context("when finding or creating resource config fails", func() {
				disaster := errors.New("no")

				BeforeEach(func() {
					fakeResourceConfigFactory.FindOrCreateResourceConfigReturns(nil, disaster)
				})

				It("returns the error", func() {
					Expect(stepErr).To(Equal(disaster))
				})
			})

			Context("when saving the build output fails", func() {
				disaster := errors.New("nope")

				BeforeEach(func() {
					fakeBuild.SaveOutputReturns(disaster)
				})

				It("returns the error", func() {
					Expect(stepErr).To(Equal(disaster))
				})
			})

			Context("when performing the put exits unsuccessfully", func() {
				BeforeEach(func() {
					fakeResource.PutReturns(nil, atc.ErrResourceScriptFailed{
						ExitStatus: 42,
					})
				})

				It("finishes the step via the delegate", func() {
					Expect(fakeDelegate.FinishedCallCount()).To(Equal(1))
					_, status, info := fakeDelegate.FinishedArgsForCall(0)
					Expect(status).To(Equal(exec.ExitStatus(42)))
					Expect(info).To(BeZero())
				})

				It("returns nil", func() {
					Expect(stepErr).ToNot(HaveOccurred())
				})

				It("is not successful", func() {
					Expect(putStep.Succeeded()).To(BeFalse())
				})
			})

			Context("when performing the put errors", func() {
				disaster := errors.New("oh no")

				BeforeEach(func() {
					fakeResource.PutReturns(nil, disaster)
				})

				It("does not finish the step via the delegate", func() {
					Expect(fakeDelegate.FinishedCallCount()).To(Equal(0))
				})

				It("returns the error", func() {
					Expect(stepErr).To(Equal(disaster))
				})

				It("is not successful", func() {
					Expect(putStep.Succeeded()).To(BeFalse())
				})
			})
		})

		Context("when the resource factory fails to create the put resource", func() {
			disaster := errors.New("nope")

			BeforeEach(func() {
				fakeResourceFactory.NewResourceReturns(nil, disaster)
			})

			It("returns the failure", func() {
				Expect(stepErr).To(Equal(disaster))
			})
		})
	})
})
