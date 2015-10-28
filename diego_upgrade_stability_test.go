package upgrade_test

import (
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("Upgrade Stability Tests", func() {
	It("Deploys V0", func() {
		By("Ensuring the V0 is not currently deployed")
		deploymentsCmd := exec.Command("bosh", "deployments")
		sess, err := Start(deploymentsCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, COMMAND_TIMEOUT).Should(Exit())

		Expect(sess).NotTo(Say("cf-warden"))
		Expect(sess).NotTo(Say("cf-warden-diego-brain-and-pals"))
		Expect(sess).NotTo(Say("cf-warden-diego-cell1"))
		Expect(sess).NotTo(Say("cf-warden-diego-cell2"))
		Expect(sess).NotTo(Say("cf-warden-diego-database"))

		By("Generating the legacy deployment manifests for 5 piece wise deployments")
		generateManifestCmd := exec.Command("./scripts/generate-manifests",
			"-d", config.DiegoReleasePath,
			"-c", config.CfReleasePath,
			"-l",
		)

		sess, err = Start(generateManifestCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, COMMAND_TIMEOUT).Should(Exit(0))

		By("Deploying CF Release")
		deployCFCmd := exec.Command("bosh", "-d", "manifests/cf.yml", "-n", "deploy")

		sess, err = Start(deployCFCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(Exit(0))
		Expect(sess).To(Say("Deployed `cf-warden' to `Bosh Lite Director'"))

		By("Deploying the Database Release")
		deployDatabaseCmd := exec.Command("bosh", "-d", "manifests/database.yml", "-n", "deploy")

		sess, err = Start(deployDatabaseCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(Exit(0))
		Expect(sess).To(Say("Deployed `cf-warden-diego-database' to `Bosh Lite Director'"))

		By("Deploying the Brain and Pals Release")
		deployBrainAndPalsCmd := exec.Command("bosh", "-d", "manifests/brain-and-pals.yml", "-n", "deploy")

		sess, err = Start(deployBrainAndPalsCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(Exit(0))
		Expect(sess).To(Say("Deployed `cf-warden-diego-brain-and-pals' to `Bosh Lite Director'"))

		By("Deploying the Cell 1 Release")
		deployCell1Cmd := exec.Command("bosh", "-d", "manifests/cell1.yml", "-n", "deploy")

		sess, err = Start(deployCell1Cmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(Exit(0))
		Expect(sess).To(Say("Deployed `cf-warden-diego-cell1' to `Bosh Lite Director'"))

		By("Deploying the Cell 2 Release")
		deployCell2Cmd := exec.Command("bosh", "-d", "manifests/cell2.yml", "-n", "deploy")

		sess, err = Start(deployCell2Cmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())

		Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(Exit(0))
		Expect(sess).To(Say("Deployed `cf-warden-diego-cell2' to `Bosh Lite Director'"))
	})
})
