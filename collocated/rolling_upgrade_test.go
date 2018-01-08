package dusts_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	auctioneerconfig "code.cloudfoundry.org/auctioneer/cmd/auctioneer/config"
	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/diego-upgrade-stability-tests/collocated/fixtures"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/lager"

	archive_helper "code.cloudfoundry.org/archiver/extractor/test_helper"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"
)

var _ = Describe("RollingUpgrade", func() {
	var (
		archiveFiles []archive_helper.ArchiveFile
	)

	Context("v0 to v1", func() {
		var (
			bbs, routeEmitter, auctioneer, rep0, rep1 ifrit.Process

			canary        *models.DesiredLRP
			canaryProcess ifrit.Process
			dependencies  ifrit.Process
		)

		BeforeEach(func() {
			fileServer, fileServerAssetsDir := ComponentMakerV1.FileServer()

			archiveFiles = fixtures.GoServerApp()
			archive_helper.CreateZipArchive(
				filepath.Join(fileServerAssetsDir, "lrp.zip"),
				archiveFiles,
			)

			dependencies = ginkgomon.Invoke(grouper.NewOrdered(os.Kill, grouper.Members{
				{"dependencies", grouper.NewParallel(os.Kill, grouper.Members{
					{"nats", ComponentMakerV1.NATS()},
					{"sql", ComponentMakerV1.SQL()},
					{"consul", ComponentMakerV1.Consul()},
					{"file-server", fileServer},
					{"garden", ComponentMakerV1.Garden()},
					{"router", ComponentMakerV1.Router()},
				})},
			}))

			bbs = ginkgomon.Invoke(ComponentMakerV0.BBS())
			routeEmitter = ginkgomon.Invoke(ComponentMakerV0.RouteEmitter())
			auctioneer = ginkgomon.Invoke(ComponentMakerV0.Auctioneer())
			rep0 = ginkgomon.Invoke(ComponentMakerV0.RepN(0))
			rep1 = ginkgomon.Invoke(ComponentMakerV0.RepN(1))

			helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())
			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			gardenClient = ComponentMakerV0.GardenClient()
			bbsClient = ComponentMakerV0.BBSClient()
			bbsServiceClient = ComponentMakerV0.BBSServiceClient(logger)

			canary = helpers.DefaultLRPCreateRequest(ComponentMakerV0.Addresses(), "dust-canary", "dust-canary", 1)
			err := bbsClient.DesireLRP(logger, canary)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(gardenClient)

			helpers.StopProcesses(
				canaryProcess,
				bbs,
				routeEmitter,
				auctioneer,
				rep0, rep1,
				dependencies,
			)

			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)
		})

		It("should consistently remain routable", func() {
			Eventually(helpers.LRPStatePoller(logger, bbsClient, canary.ProcessGuid, nil), 10*time.Second).Should(Equal(models.ActualLRPStateRunning))

			canaryProcess = ifrit.Background(NewCanaryPoller(ComponentMakerV0.Addresses().Router, helpers.DefaultHost))
			Eventually(canaryProcess.Ready(), 5*time.Second).Should(BeClosed())

			By("upgrading the bbs")
			ginkgomon.Interrupt(bbs, 5*time.Second)
			skipLocket := func(cfg *bbsconfig.BBSConfig) {
				cfg.ClientLocketConfig.LocketAddress = ""
			}
			bbs = ginkgomon.Invoke(ComponentMakerV1.BBS(skipLocket))

			By("upgrading the auctioneer, route emitter")
			ginkgomon.Interrupt(auctioneer, 5*time.Second)
			auctioneer = ginkgomon.Invoke(ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
				cfg.ClientLocketConfig.LocketAddress = ""
			}))
			ginkgomon.Interrupt(routeEmitter, 5*time.Second)
			routeEmitter = ginkgomon.Invoke(ComponentMakerV1.RouteEmitter())

			upgradeRep := func(idx int, process *ifrit.Process) {
				msg := fmt.Sprintf("Upgrading cell%d", idx)
				By(msg)

				host, portStr, _ := net.SplitHostPort(ComponentMakerV0.Addresses().Rep)
				port, err := strconv.Atoi(portStr)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				port = port + 10*idx // TODO: this is a hack based on offsetPort in components.go

				By(fmt.Sprintf("evcuating cell%d", idx))
				addr := fmt.Sprintf("http://%s:%d/evacuate", host, port)
				_, err = http.Post(addr, "", nil)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				EventuallyWithOffset(1, (*process).Wait(), 10*time.Second).Should(Receive())

				*process = ginkgomon.Invoke(ComponentMakerV1.RepN(idx))
			}

			upgradeRep(0, &rep0)
			By("checking poller is still up")
			Consistently(canaryProcess.Wait(), 5*time.Second).ShouldNot(Receive())

			upgradeRep(1, &rep1)
			By("checking poller is still up")
			Consistently(canaryProcess.Wait(), 5*time.Second).ShouldNot(Receive())
		})
	})
})

type canaryPoller struct {
	routerAddr string
	host       string
}

func NewCanaryPoller(routerAddr, host string) *canaryPoller {
	return &canaryPoller{
		routerAddr: routerAddr,
		host:       host,
	}
}

func (c *canaryPoller) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	defer GinkgoRecover()

loop:
	for {
		select {
		case <-signals:
			fmt.Println("exiting canary poller...")
			return nil

		default:
			_, status, _ := helpers.ResponseBodyAndStatusCodeFromHost(c.routerAddr, c.host)

			if status == http.StatusOK {
				break loop
			}
		}
	}

	close(ready)

	for {
		select {
		case <-signals:
			fmt.Println("exiting canary poller...")
			return nil

		default:
			_, status, err := helpers.ResponseBodyAndStatusCodeFromHost(c.routerAddr, c.host)
			if err != nil {
				return err
			}

			if status != http.StatusOK {
				return errors.New(fmt.Sprintf("request failed with status %d", status))
			}
		}
	}

	return nil
}
