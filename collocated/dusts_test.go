package dusts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	auctioneerconfig "code.cloudfoundry.org/auctioneer/cmd/auctioneer/config"
	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/localip"
	repconfig "code.cloudfoundry.org/rep/cmd/rep/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"
)

var (
	repV0UnsupportedVizziniTests = []string{"MaxPids", "CF_INSTANCE_INTERNAL_IP"}
)

var _ = Describe("Dusts", func() {
	Context("exercising the API", func() {
		var (
			plumbing                                     ifrit.Process
			bbs, routeEmitter, sshProxy, auctioneer, rep ifrit.Process
		)

		BeforeEach(func() {
			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			fileServer, _ := ComponentMakerV1.FileServer()

			exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
				cfg.ExportNetworkEnvVars = true
			}

			plumbing = ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
				{Name: "nats", Runner: ComponentMakerV1.NATS()},
				{Name: "sql", Runner: ComponentMakerV1.SQL()},
				{Name: "consul", Runner: ComponentMakerV1.Consul()},
				{Name: "file-server", Runner: fileServer},
				{Name: "garden", Runner: ComponentMakerV1.Garden()},
				{Name: "router", Runner: ComponentMakerV1.Router()},
			}))

			skipLocket := func(cfg *bbsconfig.BBSConfig) {
				cfg.ClientLocketConfig.LocketAddress = ""
			}
			fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
				cfg.AuctioneerRequireTLS = false
			}
			bbs = ginkgomon.Invoke(ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer))

			routeEmitter = ginkgomon.Invoke(ComponentMakerV0.RouteEmitter())
			auctioneer = ginkgomon.Invoke(ComponentMakerV0.Auctioneer())

			rep = ginkgomon.Invoke(ComponentMakerV0.Rep(exportNetworkConfigs))

			sshProxy = ginkgomon.Invoke(ComponentMakerV0.SSHProxy())

			helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

			helpers.StopProcesses(
				bbs,
				auctioneer,
				rep,
				routeEmitter,
				sshProxy,
				plumbing,
			)

			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)
		})

		It("runs vizzini successfully", func() {
			By("with BBS and BBS client at v1")
			{
				runVizziniTests(repV0UnsupportedVizziniTests...)
			}

			By("upgrading the auctioneer and ssh-proxy")
			{
				ginkgomon.Interrupt(auctioneer, 5*time.Second)
				ginkgomon.Interrupt(sshProxy, 5*time.Second)
				auctioneer = ginkgomon.Invoke(ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}))
				sshProxy = ginkgomon.Invoke(ComponentMakerV1.SSHProxy())

				runVizziniTests(repV0UnsupportedVizziniTests...)
			}

			By("upgrading the cell")
			{
				exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				}
				ginkgomon.Interrupt(rep, 5*time.Second)
				rep = ginkgomon.Invoke(ComponentMakerV1.Rep(exportNetworkConfigs))

				runVizziniTests()
			}

			By("upgrading the route emitter")
			{
				ginkgomon.Interrupt(routeEmitter, 5*time.Second)
				routeEmitter = ginkgomon.Invoke(ComponentMakerV1.RouteEmitter())

				runVizziniTests()
			}
		})
	})
})

func runVizziniTests(skips ...string) {
	ip, err := localip.LocalIP()
	Expect(err).NotTo(HaveOccurred())
	vizziniPath := filepath.Join(os.Getenv("GOPATH"), "src/code.cloudfoundry.org/vizzini")
	flags := []string{
		"-nodes", "2",
		"-randomizeAllSpecs",
		"-r",
		"-slowSpecThreshold", "60",
		"-skip", strings.Join(skips, "|"),
		"--",
		"-bbs-address", "https://" + ComponentMakerV1.Addresses().BBS,
		"-bbs-client-cert", ComponentMakerV1.BBSSSLConfig().ClientCert,
		"-bbs-client-key", ComponentMakerV1.BBSSSLConfig().ClientKey,
		"-ssh-address", ComponentMakerV1.Addresses().SSHProxy,
		"-ssh-password", "",
		"-routable-domain-suffix", ip + ".xip.io",
		"-host-address", ip,
	}

	cmd := exec.Command("ginkgo", flags...)
	cmd.Dir = vizziniPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Expect(cmd.Run()).To(Succeed())
}
