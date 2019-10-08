package k8s_test

import (
	. "github.com/concourse/concourse/topgun"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Main team role config", func() {
	var (
		atcEndpoint         string
		helmDeployTestFlags []string
		username            = "test-viewer"
		password            = "test-viewer"
	)

	BeforeEach(func() {
		setReleaseNameAndNamespace("mt")
		Run(nil, "kubectl", "create", "namespace", namespace)
	})

	JustBeforeEach(func() {
		deployConcourseChart(releaseName, helmDeployTestFlags...)

		waitAllPodsInNamespaceToBeReady(namespace)

		pods := getPods(namespace, "--selector=app="+releaseName+"-worker")
		Expect(pods).To(HaveLen(1))

		By("Creating the web proxy")
		atcEndpoint = getExternalUrl(namespace, releaseName+"-web")

		By("Logging in")
		fly.Login(username, password, atcEndpoint)

	})

	AfterEach(func() {
		cleanup(releaseName, namespace, nil)
	})

	Context("Adding team role config yaml to web", func() {
		BeforeEach(func() {
			helmDeployTestFlags = []string{
				`--set=worker.replicas=1`,
				`--set=web.additionalVolumes[0].name=team-role-config`,
				`--set=web.additionalVolumes[0].configMap.name=team-role-config`,
				`--set=web.additionalVolumeMounts[0].name=team-role-config`,
				`--set=web.additionalVolumeMounts[0].mountPath=/foo`,
				`--set=concourse.web.auth.mainTeam.config=/foo/team-role-config.yml`,
				`--set=secrets.localUsers=test-viewer:test-viewer`,
			}

			configMapCreationArgs := []string{
				"create",
				"configmap",
				"team-role-config",
				"--namespace=" + namespace,
				`--from-literal=team-role-config.yml=
roles:
 - name: viewer
   local:
     users: [ "test-viewer" ]
 `,
			}

			Run(nil, "kubectl", configMapCreationArgs...)

		})

		It("returns the correct user role", func() {
			userRole := fly.GetUserRole("main")
			Expect(userRole).To(HaveLen(1))
			Expect(userRole[0]).To(Equal("viewer"))
		})
	})

})
