package db_test

import (
	"errors"

	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/creds"
	"github.com/concourse/concourse/atc/db"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResourceType", func() {
	var pipeline db.Pipeline

	BeforeEach(func() {
		var (
			created bool
			err     error
		)

		pipeline, created, err = defaultTeam.SavePipeline(
			"pipeline-with-types",
			atc.Config{
				ResourceTypes: atc.ResourceTypes{
					{
						Name:   "some-type",
						Type:   "registry-image",
						Source: atc.Source{"some": "repository"},
					},
					{
						Name:       "some-other-type",
						Type:       "registry-image-ng",
						Privileged: true,
						Source:     atc.Source{"some": "other-repository"},
					},
					{
						Name:   "some-type-with-params",
						Type:   "s3",
						Source: atc.Source{"some": "repository"},
						Params: atc.Params{"unpack": "true"},
					},
					{
						Name:       "some-type-with-custom-check",
						Type:       "registry-image",
						Source:     atc.Source{"some": "repository"},
						CheckEvery: "10ms",
					},
				},
			},
			0,
			db.PipelineUnpaused,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(created).To(BeTrue())
	})

	Describe("(Pipeline).ResourceTypes", func() {
		var resourceTypes []db.ResourceType

		JustBeforeEach(func() {
			var err error
			resourceTypes, err = pipeline.ResourceTypes()
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns the resource types", func() {
			Expect(resourceTypes).To(HaveLen(4))

			ids := map[int]struct{}{}

			for _, t := range resourceTypes {
				ids[t.ID()] = struct{}{}

				switch t.Name() {
				case "some-type":
					Expect(t.Name()).To(Equal("some-type"))
					Expect(t.Type()).To(Equal("registry-image"))
					Expect(t.Source()).To(Equal(atc.Source{"some": "repository"}))
					Expect(t.Version()).To(BeNil())
				case "some-other-type":
					Expect(t.Name()).To(Equal("some-other-type"))
					Expect(t.Type()).To(Equal("registry-image-ng"))
					Expect(t.Source()).To(Equal(atc.Source{"some": "other-repository"}))
					Expect(t.Version()).To(BeNil())
					Expect(t.Privileged()).To(BeTrue())
				case "some-type-with-params":
					Expect(t.Name()).To(Equal("some-type-with-params"))
					Expect(t.Type()).To(Equal("s3"))
					Expect(t.Params()).To(Equal(atc.Params{"unpack": "true"}))
				case "some-type-with-custom-check":
					Expect(t.Name()).To(Equal("some-type-with-custom-check"))
					Expect(t.Type()).To(Equal("registry-image"))
					Expect(t.Source()).To(Equal(atc.Source{"some": "repository"}))
					Expect(t.Version()).To(BeNil())
					Expect(t.CheckEvery()).To(Equal("10ms"))
				}
			}

			Expect(ids).To(HaveLen(4))
		})

		Context("when a resource type becomes inactive", func() {
			BeforeEach(func() {
				var (
					created bool
					err     error
				)

				pipeline, created, err = defaultTeam.SavePipeline(
					"pipeline-with-types",
					atc.Config{
						ResourceTypes: atc.ResourceTypes{
							{
								Name:   "some-type",
								Type:   "registry-image",
								Source: atc.Source{"some": "repository"},
							},
						},
					},
					pipeline.ConfigVersion(),
					db.PipelineUnpaused,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(created).To(BeFalse())
			})

			It("does not return inactive resource types", func() {
				Expect(resourceTypes).To(HaveLen(1))
				Expect(resourceTypes[0].Name()).To(Equal("some-type"))
			})
		})
	})

	Describe("SetCheckError", func() {
		var resourceType db.ResourceType

		BeforeEach(func() {
			var err error
			resourceType, _, err = pipeline.ResourceType("some-type")
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when the resource is first created", func() {
			It("is not errored", func() {
				Expect(resourceType.CheckSetupError()).To(BeNil())
			})
		})

		Context("when a resource check is marked as errored", func() {
			It("is then marked as errored", func() {
				originalCause := errors.New("on fire")

				err := resourceType.SetCheckSetupError(originalCause)
				Expect(err).ToNot(HaveOccurred())

				returnedResourceType, _, err := pipeline.ResourceType("some-type")
				Expect(err).ToNot(HaveOccurred())

				Expect(returnedResourceType.CheckSetupError()).To(Equal(originalCause))
			})
		})

		Context("when a resource is cleared of check errors", func() {
			It("is not marked as errored again", func() {
				originalCause := errors.New("on fire")

				err := resourceType.SetCheckSetupError(originalCause)
				Expect(err).ToNot(HaveOccurred())

				err = resourceType.SetCheckSetupError(nil)
				Expect(err).ToNot(HaveOccurred())

				returnedResourceType, _, err := pipeline.ResourceType("some-type")
				Expect(err).ToNot(HaveOccurred())

				Expect(returnedResourceType.CheckSetupError()).To(BeNil())
			})
		})
	})

	Describe("Version", func() {
		var (
			resourceType   db.ResourceType
			version        atc.Version
			resourceConfig db.ResourceConfig
		)

		BeforeEach(func() {
			var err error
			resourceType, _, err = pipeline.ResourceType("some-type")
			Expect(err).ToNot(HaveOccurred())
			Expect(resourceType.Version()).To(BeNil())

			setupTx, err := dbConn.Begin()
			Expect(err).ToNot(HaveOccurred())

			brt := db.BaseResourceType{
				Name: "registry-image",
			}

			_, err = brt.FindOrCreate(setupTx)
			Expect(err).NotTo(HaveOccurred())
			Expect(setupTx.Commit()).To(Succeed())

			resourceConfig, err = resourceType.SetResourceConfig(logger, atc.Source{"some": "repository"}, creds.VersionedResourceTypes{})
			Expect(err).ToNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			var err error
			resourceType, _, err = pipeline.ResourceType("some-type")
			Expect(err).ToNot(HaveOccurred())

			version, err = resourceType.Version()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when the resource type has a default space", func() {
			BeforeEach(func() {
				err := resourceConfig.SaveDefaultSpace(atc.Space("space"))
				Expect(err).ToNot(HaveOccurred())

				saveVersions(resourceConfig, []atc.SpaceVersion{
					atc.SpaceVersion{
						Space:   atc.Space("space"),
						Version: atc.Version{"version": "1"},
					},
					atc.SpaceVersion{
						Space:   atc.Space("space"),
						Version: atc.Version{"version": "2"},
					},
					atc.SpaceVersion{
						Space:   atc.Space("space-2"),
						Version: atc.Version{"version-2": "1"},
					},
				})

				err = resourceConfig.SaveSpaceLatestVersion(atc.Space("space"), atc.Version{"version": "2"})
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns the version", func() {
				Expect(version).To(Equal(atc.Version{"version": "2"}))
			})

			Context("when the resource type specifies a space", func() {
				BeforeEach(func() {
					_, created, err := defaultTeam.SavePipeline(
						"pipeline-with-types",
						atc.Config{
							ResourceTypes: atc.ResourceTypes{
								{
									Name:   "some-type",
									Type:   "registry-image",
									Source: atc.Source{"some": "repository"},
									Space:  "space-2",
								},
								{
									Name:       "some-other-type",
									Type:       "registry-image-ng",
									Privileged: true,
									Source:     atc.Source{"some": "other-repository"},
								},
								{
									Name:   "some-type-with-params",
									Type:   "s3",
									Source: atc.Source{"some": "repository"},
									Params: atc.Params{"unpack": "true"},
								},
								{
									Name:       "some-type-with-custom-check",
									Type:       "registry-image",
									Source:     atc.Source{"some": "repository"},
									CheckEvery: "10ms",
								},
							},
						},
						pipeline.ConfigVersion(),
						db.PipelineNoChange,
					)
					Expect(err).ToNot(HaveOccurred())
					Expect(created).To(BeFalse())
				})

				It("returns the version within the specified space", func() {
					Expect(version).To(Equal(atc.Version{"version-2": "1"}))
				})
			})
		})

		Context("when the version does not exist", func() {
			BeforeEach(func() {
				_, created, err := defaultTeam.SavePipeline(
					"non-existant-pipeline",
					atc.Config{
						ResourceTypes: atc.ResourceTypes{
							{
								Name:   "some-type",
								Type:   "registry-image",
								Source: atc.Source{"some": "repository"},
								Space:  "unknown-space",
							},
						},
					},
					0,
					db.PipelineUnpaused,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("returns the version within the specified space", func() {
				Expect(version).To(BeNil())
			})
		})
	})
})
