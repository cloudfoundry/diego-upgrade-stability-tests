package dusts_test

import (
	"os"
	"path/filepath"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/inigo/fixtures"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"

	archive_helper "code.cloudfoundry.org/archiver/extractor/test_helper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"
)

var _ = Describe("RollingUpgrade", func() {

	setupPlumbing := func() ifrit.Process {
		fileServer, fileServerAssetsDir := ComponentMakerV1.FileServer()

		archiveFiles := fixtures.GoServerApp()
		archive_helper.CreateZipArchive(
			filepath.Join(fileServerAssetsDir, "lrp.zip"),
			archiveFiles,
		)

		return ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
			{Name: "nats", Runner: ComponentMakerV1.NATS()},
			{Name: "sql", Runner: ComponentMakerV1.SQL()},
			{Name: "consul", Runner: ComponentMakerV1.Consul()},
			{Name: "file-server", Runner: fileServer},
			{Name: "garden", Runner: ComponentMakerV1.Garden()},
			{Name: "router", Runner: ComponentMakerV1.Router()},
		}))
	}

	Context("rolling upgrade v0 to v1", func() {
		var (
			canaryPoller ifrit.Process
			plumbing     ifrit.Process
		)

		BeforeEach(func() {
			diegoV0Version := os.Getenv("DIEGO_VERSION_V0")

			switch diegoV0Version {
			case diegoGAVersion:
				ComponentMakerV0 = world.MakeV0ComponentMaker("fixtures/certs/", oldArtifacts, addresses)
				upgrader = NewGAUpgrader()
			case diegoLocketLocalREVersion:
				ComponentMakerV0 = world.MakeComponentMaker("fixtures/certs/", newArtifacts, addresses)
				upgrader = NewLocketLocalREUpgrader()
			default:
				Skip("DIEGO_VERSION_V0 not set")
			}

			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			plumbing = setupPlumbing()
			helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())

			upgrader.StartUp()

			bbsClient = ComponentMakerV0.BBSClient()
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

			upgrader.ShutDown()
			helpers.StopProcesses(canaryPoller, plumbing)

			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)
		})

		It("should consistently remain routable", func() {
			canary := helpers.DefaultLRPCreateRequest(ComponentMakerV0.Addresses(), "dust-canary", "dust-canary", 1)
			err := bbsClient.DesireLRP(logger, canary)
			Expect(err).NotTo(HaveOccurred())
			Eventually(helpers.LRPStatePoller(logger, bbsClient, canary.ProcessGuid, nil)).Should(Equal(models.ActualLRPStateRunning))

			canaryPoller = ifrit.Background(NewPoller(logger, ComponentMakerV0.Addresses().Router, helpers.DefaultHost))
			Eventually(canaryPoller.Ready()).Should(BeClosed())

			upgrader.RollingUpgrade()

			By("checking poller is still up")
			Consistently(canaryPoller.Wait()).ShouldNot(Receive())
		})
	})
})
