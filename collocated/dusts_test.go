package dusts_test

import (
	"os"
	"os/exec"
	"path/filepath"

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

var _ = Describe("Dusts", func() {
	Context("exercising the API", func() {
		var (
			plumbing ifrit.Process
		)

		BeforeEach(func() {
			// required since vizzini makes assumption about the port of file server being 8080
			fileServer, fileServerAssetsDir := ComponentMakerV1.FileServer()

			buildpackAppLifeCycleDir := filepath.Join(fileServerAssetsDir, "buildpack_app_lifecycle")
			err := os.Mkdir(buildpackAppLifeCycleDir, 0755)
			Expect(err).NotTo(HaveOccurred())
			file := ComponentMakerV1.Artifacts.Lifecycles["buildpackapplifecycle"]
			helpers.Copy(file, filepath.Join(buildpackAppLifeCycleDir, "buildpack_app_lifecycle.tgz"))

			dockerAppLifeCycleDir := filepath.Join(fileServerAssetsDir, "docker_app_lifecycle")
			err = os.Mkdir(dockerAppLifeCycleDir, 0755)
			Expect(err).NotTo(HaveOccurred())
			file = ComponentMakerV1.Artifacts.Lifecycles["dockerapplifecycle"]
			helpers.Copy(file, filepath.Join(dockerAppLifeCycleDir, "docker_app_lifecycle.tgz"))

			exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
				cfg.ExportNetworkEnvVars = true
			}

			plumbing = ginkgomon.Invoke(grouper.NewOrdered(os.Kill, grouper.Members{
				{"dependencies", grouper.NewParallel(os.Kill, grouper.Members{
					{"nats", ComponentMakerV1.NATS()},
					{"sql", ComponentMakerV1.SQL()},
					{"consul", ComponentMakerV1.Consul()},
					{"file-server", fileServer},
					{"garden", ComponentMakerV1.Garden()},
				})},
				{"locket", ComponentMakerV1.Locket()},
				{"control-plane", grouper.NewParallel(os.Kill, grouper.Members{
					{"bbs", ComponentMakerV1.BBS()},
					{"auctioneer", ComponentMakerV1.Auctioneer()},
					{"router", ComponentMakerV1.Router()},
					{"route-emitter", ComponentMakerV1.RouteEmitter()},
					{"ssh-proxy", ComponentMakerV1.SSHProxy()},
					{"rep-0", ComponentMakerV1.RepN(0, exportNetworkConfigs)}, // exporting network configs is used in container_environment_test.go
					{"rep-1", ComponentMakerV1.RepN(1, exportNetworkConfigs)},
				})},
			}))

			helpers.ConsulWaitUntilReady(ComponentMakerV1.Addresses)
			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			gardenClient = ComponentMakerV1.GardenClient()
			bbsClient = ComponentMakerV1.BBSClient()
			bbsServiceClient = ComponentMakerV1.BBSServiceClient(logger)
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(gardenClient)
			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)

			helpers.StopProcesses(plumbing)
		})

		It("runs vizzini succesfully", func() {
			ip, err := localip.LocalIP()
			Expect(err).NotTo(HaveOccurred())
			// routerHost, routerPort, err := net.SplitHostPort(componentMaker.Addresses.Router)
			// Expect(err).NotTo(HaveOccurred())
			vizziniPath := filepath.Join(os.Getenv("GOPATH"), "src/code.cloudfoundry.org/vizzini")
			flags := []string{
				"-nodes", "2", // run ginkgo in parallel
				"-randomizeAllSpecs",
				"-r",
				"-slowSpecThreshold", "10", // set threshold to 10s
				"--",
				"-bbs-address", "https://" + ComponentMakerV1.Addresses.BBS,
				"-bbs-client-cert", ComponentMakerV1.BbsSSL.ClientCert,
				"-bbs-client-key", ComponentMakerV1.BbsSSL.ClientKey,
				"-ssh-address", ComponentMakerV1.Addresses.SSHProxy,
				"-ssh-password", "",
				"-routable-domain-suffix", ip + ".xip.io",
				"-host-address", ip,
			}

			cmd := exec.Command("ginkgo", flags...)
			cmd.Dir = vizziniPath
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			// cmd.Run()
			Expect(cmd.Run()).To(Succeed())
		})
	})
})
