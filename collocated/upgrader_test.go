package dusts_test

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	auctioneerconfig "code.cloudfoundry.org/auctioneer/cmd/auctioneer/config"
	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"
	"code.cloudfoundry.org/durationjson"
	"code.cloudfoundry.org/inigo/helpers"
	repconfig "code.cloudfoundry.org/rep/cmd/rep/config"
	routeemitterconfig "code.cloudfoundry.org/route-emitter/cmd/route-emitter/config"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

func upgradeRep(idx int, process *ifrit.Process, modifyFuncs ...func(*repconfig.RepConfig)) {
	msg := fmt.Sprintf("Upgrading cell %d", idx)
	By(msg)

	host, portStr, _ := net.SplitHostPort(ComponentMakerV0.Addresses().Rep)
	port, err := strconv.Atoi(portStr)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	port = port + 10*idx // TODO: this is a hack based on offsetPort in components.go

	By(fmt.Sprintf("evacuating cell%d", idx))
	addr := fmt.Sprintf("http://%s:%d/evacuate", host, port)
	_, err = http.Post(addr, "", nil)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	EventuallyWithOffset(1, (*process).Wait()).Should(Receive())

	*process = ginkgomon.Invoke(ComponentMakerV1.RepN(idx, modifyFuncs...))
}

type Upgrader interface {
	StartUp()
	RollingUpgrade()
	ShutDown()
}

type diegoGAUpgrader struct {
	bbs          ifrit.Process
	routeEmitter ifrit.Process
	auctioneer   ifrit.Process
	rep0         ifrit.Process
	rep1         ifrit.Process
}

func NewGAUpgrader() Upgrader {
	return &diegoGAUpgrader{}
}

func (ga *diegoGAUpgrader) StartUp() {
	ga.bbs = ginkgomon.Invoke(ComponentMakerV0.BBS())
	ga.routeEmitter = ginkgomon.Invoke(ComponentMakerV0.RouteEmitter())
	ga.auctioneer = ginkgomon.Invoke(ComponentMakerV0.Auctioneer())
	ga.rep0 = ginkgomon.Invoke(ComponentMakerV0.RepN(0))
	ga.rep1 = ginkgomon.Invoke(ComponentMakerV0.RepN(1))
}

func (ga *diegoGAUpgrader) RollingUpgrade() {
	By("Upgrading the BBS")
	ginkgomon.Interrupt(ga.bbs, 5*time.Second)
	skipLocket := func(cfg *bbsconfig.BBSConfig) {
		cfg.ClientLocketConfig.LocketAddress = ""
	}
	ga.bbs = ginkgomon.Invoke(ComponentMakerV1.BBS(skipLocket))

	By("Upgrading the Auctioneer")
	ginkgomon.Interrupt(ga.auctioneer, 5*time.Second)
	ga.auctioneer = ginkgomon.Invoke(ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
		cfg.ClientLocketConfig.LocketAddress = ""
	}))

	By("Upgrading the Route Emitter")
	ginkgomon.Interrupt(ga.routeEmitter, 5*time.Second)
	ga.routeEmitter = ginkgomon.Invoke(ComponentMakerV1.RouteEmitter())

	upgradeRep(0, &ga.rep0)

	upgradeRep(1, &ga.rep1)
}

func (ga *diegoGAUpgrader) ShutDown() {
	helpers.StopProcesses(
		ga.bbs,
		ga.routeEmitter,
		ga.auctioneer,
		ga.rep0,
		ga.rep1,
	)
}

type diegoLocketLocalREUpgrader struct {
	bbs              ifrit.Process
	routeEmitter0    ifrit.Process
	routeEmitter1    ifrit.Process
	auctioneer       ifrit.Process
	rep0             ifrit.Process
	rep1             ifrit.Process
	locket           ifrit.Process
	cell0ID, cell1ID string
}

func NewLocketLocalREUpgrader() *diegoLocketLocalREUpgrader {
	return &diegoLocketLocalREUpgrader{}
}

func (lre *diegoLocketLocalREUpgrader) StartUp() {
	lre.locket = ginkgomon.Invoke(ComponentMakerV0.Locket())

	lre.bbs = ginkgomon.Invoke(ComponentMakerV0.BBS())
	lre.auctioneer = ginkgomon.Invoke(ComponentMakerV0.Auctioneer())

	lre.rep0 = ginkgomon.Invoke(ComponentMakerV0.RepN(0, func(cfg *repconfig.RepConfig) {
		cfg.EvacuationTimeout = durationjson.Duration(10 * time.Second)
		lre.cell0ID = cfg.CellID
	}))
	lre.rep1 = ginkgomon.Invoke(ComponentMakerV0.RepN(1, func(cfg *repconfig.RepConfig) {
		cfg.EvacuationTimeout = durationjson.Duration(10 * time.Second)
		lre.cell1ID = cfg.CellID
	}))

	lre.routeEmitter0 = ginkgomon.Invoke(ComponentMakerV0.RouteEmitterN(0, func(cfg *routeemitterconfig.RouteEmitterConfig) {
		cfg.CellID = lre.cell0ID
	}))
	lre.routeEmitter1 = ginkgomon.Invoke(ComponentMakerV0.RouteEmitterN(1, func(cfg *routeemitterconfig.RouteEmitterConfig) {
		cfg.CellID = lre.cell1ID
	}))
}

func (lre *diegoLocketLocalREUpgrader) ShutDown() {
	helpers.StopProcesses(
		lre.bbs,
		lre.routeEmitter0,
		lre.routeEmitter1,
		lre.auctioneer,
		lre.rep0,
		lre.rep1,
		lre.locket,
	)
}

func (lre *diegoLocketLocalREUpgrader) RollingUpgrade() {
	By("Upgrading Locket")
	ginkgomon.Interrupt(lre.locket, 5*time.Second)
	lre.locket = ginkgomon.Invoke(ComponentMakerV1.Locket())

	By("Downgrading Locket")
	ginkgomon.Interrupt(lre.locket, 5*time.Second)
	lre.locket = ginkgomon.Invoke(ComponentMakerV0.Locket())

	By("Upgrading the BBS")
	ginkgomon.Interrupt(lre.bbs, 5*time.Second)
	lre.bbs = ginkgomon.Invoke(ComponentMakerV1.BBS())

	By("Upgrading Locket")
	ginkgomon.Interrupt(lre.locket, 5*time.Second)
	lre.locket = ginkgomon.Invoke(ComponentMakerV1.Locket())

	By("Upgrading the Auctioneer")
	ginkgomon.Interrupt(lre.auctioneer, 5*time.Second)
	lre.auctioneer = ginkgomon.Invoke(ComponentMakerV1.Auctioneer())

	setEvacuationTimeout := func(cfg *repconfig.RepConfig) {
		cfg.EvacuationTimeout = durationjson.Duration(10 * time.Second)
	}

	upgradeRep(0, &lre.rep0, setEvacuationTimeout)
	By("Upgrading Route Emitter 0")
	ginkgomon.Interrupt(lre.routeEmitter0, 5*time.Second)
	lre.routeEmitter0 = ginkgomon.Invoke(ComponentMakerV1.RouteEmitterN(0, func(cfg *routeemitterconfig.RouteEmitterConfig) {
		cfg.CellID = lre.cell0ID
	}))

	upgradeRep(1, &lre.rep1, setEvacuationTimeout)
	By("Upgrading Route Emitter 1")
	ginkgomon.Interrupt(lre.routeEmitter1, 5*time.Second)
	lre.routeEmitter1 = ginkgomon.Invoke(ComponentMakerV1.RouteEmitterN(1, func(cfg *routeemitterconfig.RouteEmitterConfig) {
		cfg.CellID = lre.cell1ID
	}))
}
