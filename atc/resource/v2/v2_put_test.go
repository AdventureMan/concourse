package v2_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"

	"code.cloudfoundry.org/garden"
	gfakes "code.cloudfoundry.org/garden/gardenfakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/concourse/concourse/atc"
	v2 "github.com/concourse/concourse/atc/resource/v2"
	"github.com/concourse/concourse/atc/resource/v2/v2fakes"
)

var _ = Describe("Resource Put", func() {
	var (
		source atc.Source
		params atc.Params

		config map[string]interface{}

		outScriptStderr     string
		outScriptExitStatus int
		runOutError         error
		attachOutError      error
		putErr              error
		response            []byte

		outScriptProcess *gfakes.FakeProcess

		spaceVersions         []atc.SpaceVersion
		expectedSpaceVersions []atc.SpaceVersion
		fakePutEventHandler   *v2fakes.FakePutEventHandler

		ioConfig  atc.IOConfig
		stdoutBuf *gbytes.Buffer
		stderrBuf *gbytes.Buffer

		ctx    context.Context
		cancel func()
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		fakePutEventHandler = new(v2fakes.FakePutEventHandler)

		source = atc.Source{"some": "source"}
		params = atc.Params{"other": "params"}

		config = make(map[string]interface{})
		for k, v := range source {
			config[k] = v
		}
		for k, v := range params {
			config[k] = v
		}

		outScriptStderr = ""
		outScriptExitStatus = 0
		runOutError = nil
		attachOutError = nil

		outScriptProcess = new(gfakes.FakeProcess)
		outScriptProcess.IDReturns(v2.TaskProcessID)
		outScriptProcess.WaitStub = func() (int, error) {
			return outScriptExitStatus, nil
		}

		stdoutBuf = gbytes.NewBuffer()
		stderrBuf = gbytes.NewBuffer()

		ioConfig = atc.IOConfig{
			Stdout: stdoutBuf,
			Stderr: stderrBuf,
		}
		putErr = nil

		expectedSpaceVersions = []atc.SpaceVersion{
			{
				Space:   "some-space",
				Version: atc.Version{"ref": "v1"},
				Metadata: atc.Metadata{
					atc.MetadataField{
						Name:  "some",
						Value: "metadata",
					},
				},
			},
			{
				Space:   "some-space",
				Version: atc.Version{"ref": "v2"},
				Metadata: atc.Metadata{
					atc.MetadataField{
						Name:  "other",
						Value: "metadata",
					},
				},
			},
		}

		streamedOut := gbytes.NewBuffer()
		fakeContainer.StreamOutReturns(streamedOut, nil)

		response = []byte(`
			{"action": "created", "space": "some-space", "version": {"ref": "v1"}, "metadata": [{"name": "some", "value": "metadata"}]}
			{"action": "created", "space": "some-space", "version": {"ref": "v2"}, "metadata": [{"name": "other", "value": "metadata"}]}`)
	})

	Describe("running", func() {
		JustBeforeEach(func() {
			fakeContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (garden.Process, error) {
				if runOutError != nil {
					return nil, runOutError
				}

				_, err := io.Stderr.Write([]byte(outScriptStderr))
				Expect(err).NotTo(HaveOccurred())

				request, err := ioutil.ReadAll(io.Stdin)
				Expect(err).NotTo(HaveOccurred())

				var putReq v2.PutRequest
				err = json.Unmarshal(request, &putReq)
				Expect(err).NotTo(HaveOccurred())

				Expect(putReq.Config).To(Equal(map[string]interface{}(config)))
				Expect(putReq.ResponsePath).ToNot(BeEmpty())

				return outScriptProcess, nil
			}

			fakeContainer.AttachStub = func(processID string, io garden.ProcessIO) (garden.Process, error) {
				if attachOutError != nil {
					return nil, attachOutError
				}

				_, err := io.Stderr.Write([]byte(outScriptStderr))
				Expect(err).NotTo(HaveOccurred())

				request, err := ioutil.ReadAll(io.Stdin)
				Expect(err).NotTo(HaveOccurred())

				var putReq v2.PutRequest
				err = json.Unmarshal(request, &putReq)
				Expect(err).NotTo(HaveOccurred())

				Expect(putReq.Config).To(Equal(map[string]interface{}(config)))
				Expect(putReq.ResponsePath).ToNot(BeEmpty())

				return outScriptProcess, nil
			}

			fakePutEventHandler.CreatedResponseStub = func(space atc.Space, version atc.Version, metadata atc.Metadata, spaceVersions []atc.SpaceVersion) ([]atc.SpaceVersion, error) {
				spaceVersions = append(spaceVersions, atc.SpaceVersion{space, version, metadata})
				return spaceVersions, nil
			}

			spaceVersions, putErr = resource.Put(ctx, fakePutEventHandler, ioConfig, source, params)
		})

		Context("when out artifact has already been spawned", func() {
			It("reattaches to it", func() {
				Expect(fakeContainer.AttachCallCount()).To(Equal(1))

				pid, _ := fakeContainer.AttachArgsForCall(0)
				Expect(pid).To(Equal(v2.TaskProcessID))
			})

			It("does not run an additional process", func() {
				Expect(fakeContainer.RunCallCount()).To(BeZero())
			})

			Context("when artifact put succeeds", func() {
				BeforeEach(func() {
					tarStream := new(bytes.Buffer)

					tarWriter := tar.NewWriter(tarStream)

					err := tarWriter.WriteHeader(&tar.Header{
						Name: "doesnt matter",
						Size: int64(len(response)),
					})
					Expect(err).ToNot(HaveOccurred())

					_, err = tarWriter.Write(response)
					Expect(err).ToNot(HaveOccurred())

					err = tarWriter.Close()
					Expect(err).ToNot(HaveOccurred())

					fakeContainer.StreamOutReturns(ioutil.NopCloser(tarStream), nil)
				})

				It("returns the versions and space written to the temp file", func() {
					Expect(fakePutEventHandler.CreatedResponseCallCount()).To(Equal(2))
					space, version, metadata, spaceVersion := fakePutEventHandler.CreatedResponseArgsForCall(0)
					Expect(space).To(Equal(atc.Space("some-space")))
					Expect(version).To(Equal(atc.Version{"ref": "v1"}))
					Expect(metadata).To(Equal(atc.Metadata{
						atc.MetadataField{
							Name:  "some",
							Value: "metadata",
						},
					}))
					Expect(spaceVersion).To(HaveLen(0))

					space, version, metadata, spaceVersion = fakePutEventHandler.CreatedResponseArgsForCall(1)
					Expect(space).To(Equal(atc.Space("some-space")))
					Expect(version).To(Equal(atc.Version{"ref": "v2"}))
					Expect(metadata).To(Equal(atc.Metadata{
						atc.MetadataField{
							Name:  "other",
							Value: "metadata",
						},
					}))
					Expect(spaceVersion).To(Equal([]atc.SpaceVersion{
						{
							Space:   "some-space",
							Version: atc.Version{"ref": "v1"},
							Metadata: atc.Metadata{
								atc.MetadataField{
									Name:  "some",
									Value: "metadata",
								},
							},
						},
					}))

					Expect(putErr).ToNot(HaveOccurred())
					Expect(spaceVersions).To(HaveLen(2))
					Expect(spaceVersions).To(ConsistOf(expectedSpaceVersions))
				})
			})

			Context("when artifact put outputs to stderr", func() {
				BeforeEach(func() {
					outScriptStderr = "some stderr data"
				})

				It("emits it to the log sink", func() {
					Expect(stderrBuf).To(gbytes.Say("some stderr data"))
				})
			})

			Context("when running artifact put fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					attachOutError = disaster
					runOutError = disaster
				})

				It("returns the error", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr).To(Equal(disaster))
				})
			})

			Context("when artifact put exits nonzero", func() {
				BeforeEach(func() {
					outScriptExitStatus = 9
				})

				It("returns an err containing stdout/stderr of the process", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr.Error()).To(ContainSubstring("exit status 9"))
				})
			})
		})

		Context("when artifact put has not yet been spawned", func() {
			BeforeEach(func() {
				attachOutError = errors.New("not-found")
			})

			It("specifies the process id in the process spec", func() {
				Expect(fakeContainer.RunCallCount()).To(Equal(1))

				spec, _ := fakeContainer.RunArgsForCall(0)
				Expect(spec.ID).To(Equal(v2.TaskProcessID))
			})

			It("runs /opt/resource/out <source path> with the request on stdin", func() {
				Expect(fakeContainer.RunCallCount()).To(Equal(1))

				spec, _ := fakeContainer.RunArgsForCall(0)
				Expect(spec.Path).To(Equal("artifact put"))
				Expect(spec.Dir).To(Equal("put"))
			})

			Context("when artifact put succeeds", func() {
				BeforeEach(func() {
					tarStream := new(bytes.Buffer)

					tarWriter := tar.NewWriter(tarStream)

					err := tarWriter.WriteHeader(&tar.Header{
						Name: "doesnt matter",
						Size: int64(len(response)),
					})
					Expect(err).ToNot(HaveOccurred())

					_, err = tarWriter.Write(response)
					Expect(err).ToNot(HaveOccurred())

					err = tarWriter.Close()
					Expect(err).ToNot(HaveOccurred())

					fakeContainer.StreamOutReturns(ioutil.NopCloser(tarStream), nil)
				})

				It("returns the versions and space written to the temp file", func() {
					Expect(putErr).ToNot(HaveOccurred())
					Expect(spaceVersions).To(HaveLen(2))
					Expect(spaceVersions).To(ConsistOf(expectedSpaceVersions))
				})
			})

			Context("when artifact put outputs to stderr", func() {
				BeforeEach(func() {
					outScriptStderr = "some stderr data"
				})

				It("emits it to the log sink", func() {
					Expect(stderrBuf).To(gbytes.Say("some stderr data"))
				})
			})

			Context("when running artifact put fails", func() {
				disaster := errors.New("oh no!")

				BeforeEach(func() {
					runOutError = disaster
				})

				It("returns the error", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr).To(Equal(disaster))
				})
			})

			Context("when artifact put exits nonzero", func() {
				BeforeEach(func() {
					outScriptExitStatus = 9
				})

				It("returns an err containing stdout/stderr of the process", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr.Error()).To(ContainSubstring("exit status 9"))
				})
			})

			Context("when the response has an unknown action", func() {
				BeforeEach(func() {
					tarStream := new(bytes.Buffer)

					tarWriter := tar.NewWriter(tarStream)

					response = []byte(`
			{"action": "unknown-action", "space": "some-space", "version": {"ref": "v1"}}`)

					err := tarWriter.WriteHeader(&tar.Header{
						Name: "doesnt matter",
						Size: int64(len(response)),
					})
					Expect(err).ToNot(HaveOccurred())

					_, err = tarWriter.Write(response)
					Expect(err).ToNot(HaveOccurred())

					err = tarWriter.Close()
					Expect(err).ToNot(HaveOccurred())

					fakeContainer.StreamOutReturns(ioutil.NopCloser(tarStream), nil)
				})

				It("returns action not found error", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr).To(Equal(v2.ActionNotFoundError{Action: "unknown-action"}))
				})
			})

			Context("when the response is garbage", func() {
				BeforeEach(func() {
					tarStream := new(bytes.Buffer)

					tarWriter := tar.NewWriter(tarStream)

					response = []byte("vito")

					err := tarWriter.WriteHeader(&tar.Header{
						Name: "doesnt matter",
						Size: int64(len(response)),
					})
					Expect(err).ToNot(HaveOccurred())

					_, err = tarWriter.Write(response)
					Expect(err).ToNot(HaveOccurred())

					err = tarWriter.Close()
					Expect(err).ToNot(HaveOccurred())

					fakeContainer.StreamOutReturns(ioutil.NopCloser(tarStream), nil)
				})

				It("returns a failed to decode error", func() {
					Expect(putErr).To(HaveOccurred())
					Expect(putErr.Error()).To(ContainSubstring("failed to decode response"))
				})
			})

			Context("when streaming out fails", func() {
				BeforeEach(func() {
					fakeContainer.StreamOutReturns(nil, errors.New("ah"))
				})

				It("returns the error", func() {
					Expect(putErr).To(HaveOccurred())
				})
			})

			Context("when streaming out non tar response", func() {
				BeforeEach(func() {
					streamedOut := gbytes.NewBuffer()
					fakeContainer.StreamOutReturns(streamedOut, nil)
				})

				It("returns an error", func() {
					Expect(putErr).To(HaveOccurred())
				})
			})
		})
	})

	Context("when a signal is received", func() {
		var waited chan<- struct{}
		var done chan struct{}

		BeforeEach(func() {
			fakeContainer.AttachReturns(nil, errors.New("not-found"))
			fakeContainer.RunReturns(outScriptProcess, nil)

			waiting := make(chan struct{})
			done = make(chan struct{})
			waited = waiting

			outScriptProcess.WaitStub = func() (int, error) {
				// cause waiting to block so that it can be aborted
				<-waiting
				return 0, nil
			}

			fakeContainer.StopStub = func(bool) error {
				close(waited)
				return nil
			}

			go func() {
				spaceVersions, putErr = resource.Put(ctx, fakePutEventHandler, ioConfig, source, params)
				close(done)
			}()
		})

		It("stops the container", func() {
			cancel()
			<-done
			Expect(fakeContainer.StopCallCount()).To(Equal(1))
			Expect(fakeContainer.StopArgsForCall(0)).To(BeFalse())
			Expect(putErr).To(Equal(context.Canceled))
		})

		It("doesn't send garden terminate signal to process", func() {
			cancel()
			<-done
			Expect(putErr).To(Equal(context.Canceled))
			Expect(outScriptProcess.SignalCallCount()).To(BeZero())
		})

		Context("when container.stop returns an error", func() {
			var disaster error

			BeforeEach(func() {
				disaster = errors.New("gotta get away")

				fakeContainer.StopStub = func(bool) error {
					close(waited)
					return disaster
				}
			})

			It("masks the error", func() {
				cancel()
				<-done
				Expect(putErr).To(Equal(context.Canceled))
			})
		})
	})
})
