package dusts_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/serviceclient"
	"code.cloudfoundry.org/consuladapter/consulrunner"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/helpers/certauthority"
	"code.cloudfoundry.org/inigo/helpers/portauthority"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

const (
	diegoGAVersion            = "v1.0.0"
	diegoLocketLocalREVersion = "v1.25.2"
)

var (
	ComponentMakerV0, ComponentMakerV1 world.ComponentMaker

	componentLogs *os.File

	oldArtifacts, newArtifacts world.BuiltArtifacts
	addresses                  world.ComponentAddresses
	upgrader                   Upgrader

	bbsClient        bbs.InternalClient
	bbsServiceClient serviceclient.ServiceClient
	logger           lager.Logger
	allocator        portauthority.PortAllocator
	certAuthority    certauthority.CertAuthority

	depotDir string
)

func TestDusts(t *testing.T) {
	helpers.RegisterDefaultTimeouts()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dusts Suite")
}

var _ = BeforeSuite(func() {
	if version := os.Getenv("DIEGO_VERSION_V0"); version != diegoGAVersion && version != diegoLocketLocalREVersion {
		Fail("DIEGO_VERSION_V0 not set")
	}

	oldArtifacts = world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	oldArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	oldArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	oldArtifacts.Executables = compileTestedExecutablesV0()

	newArtifacts = world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	newArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	newArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	newArtifacts.Executables = compileTestedExecutablesV1()

	_, dbBaseConnectionString := world.DBInfo()

	// TODO: the hard coded addresses for router and file server prevent running multiple dusts tests at the same time
	addresses = world.ComponentAddresses{
		Garden:              fmt.Sprintf("127.0.0.1:%d", 10000+config.GinkgoConfig.ParallelNode),
		NATS:                fmt.Sprintf("127.0.0.1:%d", 11000+config.GinkgoConfig.ParallelNode),
		Consul:              fmt.Sprintf("127.0.0.1:%d", 12750+config.GinkgoConfig.ParallelNode*consulrunner.PortOffsetLength),
		Rep:                 fmt.Sprintf("127.0.0.1:%d", 14000+config.GinkgoConfig.ParallelNode),
		FileServer:          fmt.Sprintf("127.0.0.1:%d", 8080),
		Router:              fmt.Sprintf("127.0.0.1:%d", 80),
		BBS:                 fmt.Sprintf("127.0.0.1:%d", 20500+config.GinkgoConfig.ParallelNode*2),
		Health:              fmt.Sprintf("127.0.0.1:%d", 20500+config.GinkgoConfig.ParallelNode*2+1),
		Auctioneer:          fmt.Sprintf("127.0.0.1:%d", 23000+config.GinkgoConfig.ParallelNode),
		SSHProxy:            fmt.Sprintf("127.0.0.1:%d", 23500+config.GinkgoConfig.ParallelNode),
		SSHProxyHealthCheck: fmt.Sprintf("127.0.0.1:%d", 24500+config.GinkgoConfig.ParallelNode),
		FakeVolmanDriver:    fmt.Sprintf("127.0.0.1:%d", 25500+config.GinkgoConfig.ParallelNode),
		Locket:              fmt.Sprintf("127.0.0.1:%d", 26500+config.GinkgoConfig.ParallelNode),
		SQL:                 fmt.Sprintf("%sdiego_%d", dbBaseConnectionString, config.GinkgoConfig.ParallelNode),
	}

	node := GinkgoParallelNode()
	startPort := 2000 * node
	portRange := 5000
	endPort := startPort + portRange

	allocator, err := portauthority.New(startPort, endPort)
	Expect(err).NotTo(HaveOccurred())

	depotDir, err = ioutil.TempDir("", "depotDir")
	Expect(err).NotTo(HaveOccurred())

	certAuthority, err = certauthority.NewCertAuthority(depotDir, "ca")
	Expect(err).NotTo(HaveOccurred())

	componentLogPath := os.Getenv("DUSTS_COMPONENT_LOG_PATH")
	if componentLogPath == "" {
		componentLogPath = fmt.Sprintf("dusts-component-logs.0.0.0.%d.log", time.Now().Unix())
	}
	componentLogs, err = os.Create(componentLogPath)
	Expect(err).NotTo(HaveOccurred())
	fmt.Printf("Writing component logs to %s\n", componentLogPath)

	ComponentMakerV1 = world.MakeComponentMaker(newArtifacts, addresses, allocator, certAuthority)
	ComponentMakerV1.Setup()

	oldGinkgoWriter := GinkgoWriter
	GinkgoWriter = componentLogs
	defer func() {
		GinkgoWriter = oldGinkgoWriter
	}()
	ComponentMakerV1.GrootFSInitStore()
})

var _ = AfterSuite(func() {
	oldGinkgoWriter := GinkgoWriter
	GinkgoWriter = componentLogs
	defer func() {
		GinkgoWriter = oldGinkgoWriter
	}()
	if ComponentMakerV1 != nil {
		ComponentMakerV1.GrootFSDeleteStore()
	}

	Expect(os.RemoveAll(depotDir)).To(Succeed())
	componentLogs.Close()
})

func QuietBeforeEach(f func()) {
	BeforeEach(func() {
		oldGinkgoWriter := GinkgoWriter
		GinkgoWriter = componentLogs
		defer func() {
			GinkgoWriter = oldGinkgoWriter
		}()
		f()
	})
}

func QuietJustBeforeEach(f func()) {
	JustBeforeEach(func() {
		oldGinkgoWriter := GinkgoWriter
		GinkgoWriter = componentLogs
		defer func() {
			GinkgoWriter = oldGinkgoWriter
		}()
		f()
	})
}

func lazyBuild(binariesPath, gopath, packagePath string, args ...string) string {
	Expect(os.MkdirAll(binariesPath, 0777)).To(Succeed())
	binaryName := filepath.Base(packagePath)
	expectedBinaryPath := path.Join(binariesPath, binaryName)
	if _, err := os.Stat(expectedBinaryPath); os.IsNotExist(err) {
		fmt.Printf("Building %s\n", packagePath)
		binaryPath, err := gexec.BuildIn(gopath, packagePath, args...)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Rename(binaryPath, path.Join(binariesPath, binaryName))).To(Succeed())
	}
	return expectedBinaryPath
}

func compileTestedExecutablesV1() world.BuiltExecutables {
	fmt.Println("Lazily building V1 executables")
	binariesPath := "/tmp/v1_binaries"
	builtExecutables := world.BuiltExecutables{}

	builtExecutables["garden"] = lazyBuild(binariesPath, os.Getenv("GARDEN_GOPATH"), "code.cloudfoundry.org/guardian/cmd/gdn", "-race", "-a", "-tags", "daemon")
	builtExecutables["auctioneer"] = lazyBuild(binariesPath, os.Getenv("AUCTIONEER_GOPATH"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = lazyBuild(binariesPath, os.Getenv("REP_GOPATH"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = lazyBuild(binariesPath, os.Getenv("BBS_GOPATH"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["locket"] = lazyBuild(binariesPath, os.Getenv("LOCKET_GOPATH"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	builtExecutables["file-server"] = lazyBuild(binariesPath, os.Getenv("FILE_SERVER_GOPATH"), "code.cloudfoundry.org/fileserver/cmd/file-server", "-race")
	builtExecutables["route-emitter"] = lazyBuild(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	builtExecutables["router"] = lazyBuild(binariesPath, os.Getenv("ROUTER_GOPATH"), "code.cloudfoundry.org/gorouter", "-race")
	builtExecutables["routing-api"] = lazyBuild(binariesPath, os.Getenv("ROUTING_API_GOPATH"), "code.cloudfoundry.org/routing-api/cmd/routing-api", "-race")
	builtExecutables["ssh-proxy"] = lazyBuild(binariesPath, os.Getenv("SSH_PROXY_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = lazyBuild(binariesPath, os.Getenv("SSHD_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	return builtExecutables
}

func compileTestedExecutablesV0() world.BuiltExecutables {
	fmt.Println("Lazily building V0 executables")
	binariesPath := "/tmp/v0_binaries"
	builtExecutables := world.BuiltExecutables{}

	builtExecutables["auctioneer"] = lazyBuild(binariesPath, os.Getenv("AUCTIONEER_GOPATH_V0"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = lazyBuild(binariesPath, os.Getenv("REP_GOPATH_V0"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = lazyBuild(binariesPath, os.Getenv("BBS_GOPATH_V0"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["route-emitter"] = lazyBuild(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH_V0"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	builtExecutables["ssh-proxy"] = lazyBuild(binariesPath, os.Getenv("SSH_PROXY_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	if os.Getenv("DIEGO_VERSION_V0") == diegoLocketLocalREVersion {
		builtExecutables["locket"] = lazyBuild(binariesPath, os.Getenv("GOPATH_V0"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	}

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = lazyBuild(binariesPath, os.Getenv("SSHD_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	return builtExecutables
}
