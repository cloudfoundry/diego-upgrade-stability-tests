package upgrade_test

import (
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("Upgrade Stability Tests", func() {
	var sess *Session
	var err error

	It("Deploys V0", func() {
		By("Ensuring the V0 is not currently deployed")
		deploymentsCmd := bosh("deployments")
		sess, err = Start(deploymentsCmd, GinkgoWriter, GinkgoWriter)
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
		boshCmd("manifests/cf.yml", "deploy", "Deployed `cf-warden'")

		By("Deploying the Database Release")
		boshCmd("manifests/database.yml", "deploy", "Deployed `cf-warden-diego-database'")

		By("Deploying the Brain and Pals Release")
		boshCmd("manifests/brain-and-pals.yml", "deploy", "Deployed `cf-warden-diego-brain-and-pals'")

		By("Deploying the Cell 1 Release")
		boshCmd("manifests/cell1.yml", "deploy", "Deployed `cf-warden-diego-cell1'")

		By("Deploying the Cell 2 Release")
		boshCmd("manifests/cell2.yml", "deploy", "Deployed `cf-warden-diego-cell2'")
	})

	It("Upgrades to V-prime", func() {
		By("Generating the V-prime deployment manifests for 5 piece wise deployments")
		generateManifestCmd := exec.Command("./scripts/generate-manifests",
			"-d", config.DiegoReleasePath,
			"-c", config.CfReleasePath,
		)
		sess, err := Start(generateManifestCmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, COMMAND_TIMEOUT).Should(Exit(0))

		// Roll the Diego Database
		// ************************************************************ //
		// UPGRADE D1
		By("Deploying the Database Release")
		boshCmd("manifests/database.yml", "deploy", "Deployed `cf-warden-diego-database'")

		By("Running Smoke Tests #1")
		smokeTestDiego()

		// Rolling some cells, and turning off the other in order to
		// test the new database, new cells, old brain and CF
		// ************************************************************ //
		//UPGRADE D3
		By("Deploying the Cell 1 Release")
		boshCmd("manifests/cell1.yml", "deploy", "Deployed `cf-warden-diego-cell1'")

		// AFTER UPGRADING D3, STOP D4
		By("Stopping the Cell 2 Deployment")
		boshCmd("manifests/cell2.yml", "stop cell_z2 --force", "cell_z2/0 has been stopped")

		By("Running Smoke Tests #2")
		smokeTestDiego()

		// Rolling the Brain, but turning of the new cells and turning back on
		// the old cells to test when everything on diego has rolled except the cells.
		// ************************************************************ //
		// UPGRADE D2
		By("Deploying the Brain and Pals Release")
		boshCmd("manifests/brain-and-pals.yml", "deploy", "Deployed `cf-warden-diego-brain-and-pals'")

		// START D4
		By("Starting the Cell 2 Deployment")
		boshCmd("manifests/cell2.yml", "start cell_z2", "cell_z2/0 has been started")

		// AND STOP D3
		By("Stopping the Cell 1 Deployment")
		boshCmd("manifests/cell1.yml", "stop cell_z1", "cell_z1/0 has been stopped")

		By("Running Smoke Tests #3")
		smokeTestDiego()

		// Roll CF to test when everything is upgraded except the cells.
		// ************************************************************ //
		// UPGRADE CF
		By("Deploying CF Release")
		boshCmd("manifests/cf.yml", "deploy", "Deployed `cf-warden'")

		By("Running Smoke Tests #4")
		smokeTestDiego()

		// Roll the rest of the cells and test that everything is now stable at the
		// new deployment.
		// ************************************************************ //
		// BEFORE UPGRADING D4, START D3
		By("Start the Cell 1 Deployment")
		boshCmd("manifests/cell1.yml", "start cell_z1", "cell_z1/0 has been started")

		// UPGRADE D4
		By("Deploying the Cell 2 Release")
		boshCmd("manifests/cell2.yml", "deploy", "Deployed `cf-warden-diego-cell2'")

		By("Running Smoke Tests #5")
		smokeTestDiego()
	})
})
