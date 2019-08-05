package dusts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	auctioneerconfig "code.cloudfoundry.org/auctioneer/cmd/auctioneer/config"
	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"
	"code.cloudfoundry.org/guardian/gqt/runner"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/localip"
	repconfig "code.cloudfoundry.org/rep/cmd/rep/config"
	routeemitterconfig "code.cloudfoundry.org/route-emitter/cmd/route-emitter/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"
)

var (
	repV0UnsupportedVizziniTests = []string{"MaxPids", "CF_INSTANCE_INTERNAL_IP", "sidecar"}
	// security_group_tests in V0 vizzini won't pass since they try to access the
	// router (as opposed to www.example.com in recent versions). Security groups
	// don't affect access to the host machine, therefore they cannot block
	// traffic which causes both tests in that file to fail
	securityGroupV0Tests = "should allow access to an internal IP"
)

var _ = Describe("UpgradeVizzini", func() {
	disableAuctioneerSSL := func(cfg *auctioneerconfig.AuctioneerConfig) {
		cfg.CACertFile = ""
		cfg.ServerCertFile = ""
		cfg.ServerKeyFile = ""
	}
	skipLocketForBBS := func(cfg *bbsconfig.BBSConfig) {
		cfg.LocksLocketEnabled = false
		cfg.CellRegistrationsLocketEnabled = false
	}
	fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
		cfg.AuctioneerRequireTLS = false
	}
	disableLocketForAuctioneer := func(cfg *auctioneerconfig.AuctioneerConfig) {
		cfg.LocksLocketEnabled = false
	}
	exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
		cfg.ExportNetworkEnvVars = true
	}
	var (
		plumbing                                             ifrit.Process
		locket, bbs, routeEmitter, sshProxy, auctioneer, rep ifrit.Process
		locketRunner                                         ifrit.Runner
		bbsRunner                                            ifrit.Runner
		routeEmitterRunner                                   ifrit.Runner
		sshProxyRunner                                       ifrit.Runner
		auctioneerRunner                                     ifrit.Runner
		repRunner                                            ifrit.Runner
		bbsClientGoPathEnvVar                                string
		setRouteEmitterCellID                                func(config *routeemitterconfig.RouteEmitterConfig)
	)

	if os.Getenv("DIEGO_VERSION_V0") == diegoGAVersion {
		Context(fmt.Sprintf("from %s", diegoGAVersion), func() {
			QuietBeforeEach(func() {
				logger = lager.NewLogger("test")
				logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

				bbsClientGoPathEnvVar = "GOPATH_V0"

				ComponentMakerV0 = world.MakeV0ComponentMaker(oldArtifacts, addresses, allocator, certAuthority)
				ComponentMakerV0.Setup()

				fileServer, _ := ComponentMakerV1.FileServer()

				plumbing = ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
					{Name: "nats", Runner: ComponentMakerV1.NATS()},
					{Name: "sql", Runner: ComponentMakerV1.SQL()},
					{Name: "consul", Runner: ComponentMakerV1.Consul()},
					{Name: "file-server", Runner: fileServer},
					{Name: "garden", Runner: ComponentMakerV1.Garden(func(cfg *runner.GdnRunnerConfig) {
						poolSize := 100
						cfg.PortPoolSize = &poolSize
					})},
					{Name: "router", Runner: ComponentMakerV1.Router()},
				}))
				helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())

				bbsRunner = ComponentMakerV0.BBS()
				routeEmitterRunner = ComponentMakerV0.RouteEmitter()
				auctioneerRunner = ComponentMakerV0.Auctioneer()
				repRunner = ComponentMakerV0.Rep()
				sshProxyRunner = ComponentMakerV0.SSHProxy()
			})

			QuietJustBeforeEach(func() {
				bbs = ginkgomon.Invoke(bbsRunner)
				routeEmitter = ginkgomon.Invoke(routeEmitterRunner)
				auctioneer = ginkgomon.Invoke(auctioneerRunner)
				rep = ginkgomon.Invoke(repRunner)
				sshProxy = ginkgomon.Invoke(sshProxyRunner)
			})

			AfterEach(func() {
				destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

				helpers.StopProcesses(
					auctioneer,
					rep,
					routeEmitter,
					sshProxy,
					bbs,
					plumbing,
				)

				Expect(destroyContainerErrors).To(
					BeEmpty(),
					"%d containers failed to be destroyed!",
					len(destroyContainerErrors),
				)
			})

			Context("v0 configuration", func() {
				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV0.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the BBS API", func() {
				BeforeEach(func() {
					bbsRunner = ComponentMakerV1.BBS(skipLocketForBBS, fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV0.Auctioneer(disableAuctioneerSSL)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the BBS API and BBS client", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"

					bbsRunner = ComponentMakerV1.BBS(skipLocketForBBS, fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV0.Auctioneer(disableAuctioneerSSL)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
				})
			})

			Context("upgrading the BBS API, BBS client, sshProxy, and Auctioneer", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					bbsRunner = ComponentMakerV1.BBS(skipLocketForBBS, fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV1.Auctioneer(disableLocketForAuctioneer, disableAuctioneerSSL)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
				})
			})

			Context("upgrading the BBS API, BBS client, sshProxy, Auctioneer, and Rep", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					bbsRunner = ComponentMakerV1.BBS(skipLocketForBBS, fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV1.Auctioneer(disableLocketForAuctioneer, disableAuctioneerSSL)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
					repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar)
				})
			})

			Context("upgrading the BBS API, BBS client, sshProxy, Auctioneer, Rep, and Route Emitter", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					bbsRunner = ComponentMakerV1.BBS(skipLocketForBBS, fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV1.Auctioneer(disableLocketForAuctioneer)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
					repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
					routeEmitterRunner = ComponentMakerV1.RouteEmitter()
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar)
				})
			})
		})
	} else if os.Getenv("DIEGO_VERSION_V0") == diegoLocketLocalREVersion {
		Context(fmt.Sprintf("from %s", diegoLocketLocalREVersion), func() {
			QuietBeforeEach(func() {
				logger = lager.NewLogger("test")
				logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

				bbsClientGoPathEnvVar = "GOPATH_V0"

				ComponentMakerV0 = world.MakeComponentMaker(oldArtifacts, addresses, allocator, certAuthority)
				ComponentMakerV0.Setup()

				fileServer, _ := ComponentMakerV1.FileServer()

				plumbing = ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
					{Name: "nats", Runner: ComponentMakerV1.NATS()},
					{Name: "sql", Runner: ComponentMakerV1.SQL()},
					{Name: "consul", Runner: ComponentMakerV1.Consul()},
					{Name: "file-server", Runner: fileServer},
					{Name: "garden", Runner: ComponentMakerV1.Garden(func(cfg *runner.GdnRunnerConfig) {
						poolSize := 100
						cfg.PortPoolSize = &poolSize
					})},
					{Name: "router", Runner: ComponentMakerV1.Router()},
				}))
				helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())

				locketRunner = ComponentMakerV0.Locket()
				bbsRunner = ComponentMakerV0.BBS()
				setRouteEmitterCellID = func(config *routeemitterconfig.RouteEmitterConfig) {
					config.CellID = "the-cell-id-" + strconv.Itoa(GinkgoParallelNode()) + "-" + strconv.Itoa(0)
				}
				routeEmitterRunner = ComponentMakerV0.RouteEmitterN(0, setRouteEmitterCellID)
				auctioneerRunner = ComponentMakerV0.Auctioneer()
				repRunner = ComponentMakerV0.Rep(func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				})
				sshProxyRunner = ComponentMakerV0.SSHProxy()
			})

			QuietJustBeforeEach(func() {
				locket = ginkgomon.Invoke(locketRunner)
				bbs = ginkgomon.Invoke(bbsRunner)
				routeEmitter = ginkgomon.Invoke(routeEmitterRunner)
				auctioneer = ginkgomon.Invoke(auctioneerRunner)
				rep = ginkgomon.Invoke(repRunner)
				sshProxy = ginkgomon.Invoke(sshProxyRunner)
			})

			AfterEach(func() {
				destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

				helpers.StopProcesses(
					auctioneer,
					rep,
					routeEmitter,
					sshProxy,
					bbs,
					locket,
					plumbing,
				)

				Expect(destroyContainerErrors).To(
					BeEmpty(),
					"%d containers failed to be destroyed!",
					len(destroyContainerErrors),
				)
			})

			Context("v0 configuration", func() {
				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV0.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the Locket API", func() {
				BeforeEach(func() {
					locketRunner = ComponentMakerV1.Locket()
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV0.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the BBS API", func() {
				BeforeEach(func() {
					bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV0.Auctioneer(disableAuctioneerSSL)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the Locket and BBS API", func() {
				BeforeEach(func() {
					locketRunner = ComponentMakerV1.Locket()
					bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV0.Auctioneer(disableAuctioneerSSL)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, securityGroupV0Tests)
				})
			})

			Context("upgrading the Locket, BBS API and BBS client", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					locketRunner = ComponentMakerV1.Locket()
					bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV0.Auctioneer(disableAuctioneerSSL)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
				})
			})

			Context("upgrading the Locket, BBS API, BBS client, sshProxy, and Auctioneer", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					locketRunner = ComponentMakerV1.Locket()
					sshProxyRunner = ComponentMakerV1.SSHProxy()
					bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV1.Auctioneer(disableLocketForAuctioneer)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
				})
			})

			Context("upgrading the Locket, BBS API, BBS client, sshProxy, Auctioneer, and Rep", func() {
				BeforeEach(func() {
					bbsClientGoPathEnvVar = "GOPATH"
					fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
						cfg.AuctioneerRequireTLS = false
					}
					locketRunner = ComponentMakerV1.Locket()
					bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
					auctioneerRunner = ComponentMakerV1.Auctioneer(disableLocketForAuctioneer)
					sshProxyRunner = ComponentMakerV1.SSHProxy()
					repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
					routeEmitterRunner = ComponentMakerV1.RouteEmitterN(0, setRouteEmitterCellID)
				})

				It("runs vizzini successfully", func() {
					runVizziniTests(ComponentMakerV1.BBSSSLConfig(), bbsClientGoPathEnvVar)
				})
			})
		})
	}
})

func runVizziniTests(sslConfig world.SSLConfig, gopathEnvVar string, skips ...string) {
	ip, err := localip.LocalIP()
	Expect(err).NotTo(HaveOccurred())
	vizziniPath := filepath.Join(os.Getenv(gopathEnvVar), "src/code.cloudfoundry.org/vizzini")
	defaultRootFS := os.Getenv("DEFAULT_ROOTFS")
	flags := []string{
		"-nodes", "4",
		"-randomizeAllSpecs",
		"-r",
		"-slowSpecThreshold", "60",
		"-skip", strings.Join(skips, "|"),
		"--",
		"-bbs-address", "https://" + ComponentMakerV1.Addresses().BBS,
		"-bbs-client-cert", sslConfig.ClientCert,
		"-bbs-client-key", sslConfig.ClientKey,
		"-ssh-address", ComponentMakerV1.Addresses().SSHProxy,
		"-ssh-password", "",
		"-routable-domain-suffix", "test.internal", // Served by dnsmasq using setup_inigo script
		"-host-address", ip,
		"-default-rootfs", defaultRootFS,
	}

	env := os.Environ()
	env = append(env, fmt.Sprintf("GOPATH=%s", os.Getenv(gopathEnvVar)))
	cmd := exec.Command("ginkgo", flags...)
	cmd.Env = env
	cmd.Dir = vizziniPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Expect(cmd.Run()).To(Succeed())
}
