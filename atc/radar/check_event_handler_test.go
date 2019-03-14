package radar_test

import (
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/db/dbfakes"
	"github.com/concourse/concourse/atc/radar"
	v2 "github.com/concourse/concourse/atc/resource/v2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check Event Handler", func() {
	var (
		logger                  lager.Logger
		handler                 v2.CheckEventHandler
		spaces                  map[atc.Space]atc.Version
		fakeTx                  *dbfakes.FakeTx
		fakeResourceConfigScope *dbfakes.FakeResourceConfigScope
	)

	BeforeEach(func() {
		fakeTx = new(dbfakes.FakeTx)
		fakeResourceConfigScope = new(dbfakes.FakeResourceConfigScope)
		logger = lagertest.NewTestLogger("test")
		spaces = make(map[atc.Space]atc.Version)
	})

	JustBeforeEach(func() {
		handler = radar.NewCheckEventHandler(logger, fakeTx, fakeResourceConfigScope, spaces)
	})

	Describe("DefaultSpace", func() {
		var space atc.Space

		JustBeforeEach(func() {
			err := handler.DefaultSpace(space)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when the space is not empty", func() {
			BeforeEach(func() {
				space = atc.Space("space")
			})

			It("saves the default space", func() {
				Expect(fakeResourceConfigScope.SaveDefaultSpaceCallCount()).To(Equal(1))
				space := fakeResourceConfigScope.SaveDefaultSpaceArgsForCall(0)
				Expect(space).To(Equal(atc.Space("space")))
			})
		})

		Context("when the space is empty", func() {
			BeforeEach(func() {
				space = atc.Space("")
			})

			It("does not save the space", func() {
				Expect(fakeResourceConfigScope.SaveDefaultSpaceCallCount()).To(Equal(0))
			})
		})
	})

	Describe("Discovered", func() {
		var (
			space    atc.Space
			version  atc.Version
			metadata atc.Metadata
		)

		BeforeEach(func() {
			space = atc.Space("space")
			version = atc.Version{"ref": "v2"}
			metadata = atc.Metadata{atc.MetadataField{Name: "name", Value: "value"}}
		})

		JustBeforeEach(func() {
			err := handler.Discovered(space, version, metadata)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when the space does not exist", func() {
			It("saves the space", func() {
				Expect(fakeResourceConfigScope.SaveSpaceCallCount()).To(Equal(1))
				savedSpace := fakeResourceConfigScope.SaveSpaceArgsForCall(0)
				Expect(savedSpace).To(Equal(space))
			})

			It("updates the handler spaces", func() {
				Expect(spaces).To(Equal(map[atc.Space]atc.Version{space: version}))
			})
		})

		Context("when the space exists", func() {
			BeforeEach(func() {
				spaces = map[atc.Space]atc.Version{
					atc.Space("space"):       atc.Version{"ref": "v1"},
					atc.Space("other-space"): atc.Version{"ref": "v1"},
				}
			})

			It("does not save the space", func() {
				Expect(fakeResourceConfigScope.SaveSpaceCallCount()).To(Equal(0))
			})

			It("updates the handler spaces", func() {
				Expect(spaces).To(HaveLen(2))
				Expect(spaces).To(Equal(map[atc.Space]atc.Version{space: version, atc.Space("other-space"): atc.Version{"ref": "v1"}}))
			})
		})

		It("saves the version", func() {
			Expect(fakeResourceConfigScope.SavePartialVersionCallCount()).To(Equal(1))
			actualSpace, actualVersion, actualMetadata := fakeResourceConfigScope.SavePartialVersionArgsForCall(0)
			Expect(actualSpace).To(Equal(space))
			Expect(actualVersion).To(Equal(version))
			Expect(actualMetadata).To(Equal(metadata))
		})
	})

	Describe("LatestVersions", func() {
		JustBeforeEach(func() {
			err := handler.LatestVersions()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when the handler spaces is empty", func() {
			It("does not save the latest versions", func() {
				Expect(fakeResourceConfigScope.SaveSpaceLatestVersionCallCount()).To(Equal(0))
			})
		})

		Context("when the handler spaces contain latest versions", func() {
			BeforeEach(func() {
				spaces = map[atc.Space]atc.Version{
					atc.Space("space"):       atc.Version{"ref": "v1"},
					atc.Space("other-space"): atc.Version{"ref": "v2"},
				}
			})

			It("saves the latest versions", func() {
				Expect(fakeResourceConfigScope.SaveSpaceLatestVersionCallCount()).To(Equal(2))

				space, version := fakeResourceConfigScope.SaveSpaceLatestVersionArgsForCall(0)
				Expect(space).To(Equal(atc.Space("space")))
				Expect(version).To(Equal(atc.Version{"ref": "v1"}))

				space, version = fakeResourceConfigScope.SaveSpaceLatestVersionArgsForCall(1)
				Expect(space).To(Equal(atc.Space("other-space")))
				Expect(version).To(Equal(atc.Version{"ref": "v2"}))
			})
		})
	})
})
