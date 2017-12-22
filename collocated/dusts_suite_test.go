package dusts_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	repconfig "code.cloudfoundry.org/rep/cmd/rep/config"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/serviceclient"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/localip"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"

	"testing"
)

var (
	componentMaker world.ComponentMaker

	plumbing         ifrit.Process
	gardenClient     garden.Client
	bbsClient        bbs.InternalClient
	bbsServiceClient serviceclient.ServiceClient
	logger           lager.Logger
	localIP          string

	fileServerAssetsDir string
)

func TestDusts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dusts Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	artifacts := world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	artifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	artifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	artifacts.Executables = CompileTestedExecutables()
	artifacts.Healthcheck = CompileHealthcheckExecutable()
	CompileLdsListenerExecutable()

	payload, err := json.Marshal(artifacts)
	Expect(err).NotTo(HaveOccurred())

	return payload
}, func(encodedBuiltArtifacts []byte) {
	var builtArtifacts world.BuiltArtifacts

	err := json.Unmarshal(encodedBuiltArtifacts, &builtArtifacts)
	Expect(err).NotTo(HaveOccurred())

	localIP, err = localip.LocalIP()
	Expect(err).NotTo(HaveOccurred())

	componentMaker = helpers.MakeComponentMaker("fixtures/certs/", builtArtifacts, localIP)
	componentMaker.Setup()
})

var _ = AfterSuite(func() {
	componentMaker.Teardown()
})

var _ = BeforeEach(func() {
	// TODO: the following hard coded addresses prevents running multiple dusts tests at the same time
	componentMaker.Addresses.Router = localIP + ":80"
	componentMaker.Addresses.FileServer = "127.0.0.1:8080"

	var fileServer ifrit.Runner

	// required since vizzini makes assumption about the port of file server being 8080
	fileServer, fileServerAssetsDir = componentMaker.FileServer()
	buildpackAppLifeCycleDir := filepath.Join(fileServerAssetsDir, "buildpack_app_lifecycle")
	err := os.Mkdir(buildpackAppLifeCycleDir, 0755)
	Expect(err).NotTo(HaveOccurred())
	file := componentMaker.Artifacts.Lifecycles["buildpackapplifecycle"]
	helpers.Copy(file, filepath.Join(buildpackAppLifeCycleDir, "buildpack_app_lifecycle.tgz"))

	dockerAppLifeCycleDir := filepath.Join(fileServerAssetsDir, "docker_app_lifecycle")
	err = os.Mkdir(dockerAppLifeCycleDir, 0755)
	Expect(err).NotTo(HaveOccurred())
	file = componentMaker.Artifacts.Lifecycles["dockerapplifecycle"]
	helpers.Copy(file, filepath.Join(dockerAppLifeCycleDir, "docker_app_lifecycle.tgz"))

	exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
		cfg.ExportNetworkEnvVars = true
	}

	plumbing = ginkgomon.Invoke(grouper.NewOrdered(os.Kill, grouper.Members{
		{"dependencies", grouper.NewParallel(os.Kill, grouper.Members{
			{"nats", componentMaker.NATS()},
			{"sql", componentMaker.SQL()},
			{"consul", componentMaker.Consul()},
			{"file-server", fileServer},
			{"garden", componentMaker.Garden()},
		})},
		{"locket", componentMaker.Locket()},
		{"control-plane", grouper.NewParallel(os.Kill, grouper.Members{
			{"bbs", componentMaker.BBS()},
			{"auctioneer", componentMaker.Auctioneer()},
			{"router", componentMaker.Router()},
			{"route-emitter", componentMaker.RouteEmitter()},
			{"ssh-proxy", componentMaker.SSHProxy()},
			{"rep-0", componentMaker.RepN(0, exportNetworkConfigs)}, // exporting network configs is used in container_environment_test.go
			{"rep-1", componentMaker.RepN(1, exportNetworkConfigs)},
		})},
	}))

	helpers.ConsulWaitUntilReady()
	logger = lager.NewLogger("test")
	logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

	gardenClient = componentMaker.GardenClient()
	bbsClient = componentMaker.BBSClient()
	bbsServiceClient = componentMaker.BBSServiceClient(logger)
})

var _ = AfterEach(func() {
	destroyContainerErrors := helpers.CleanupGarden(gardenClient)

	helpers.StopProcesses(plumbing)

	Expect(destroyContainerErrors).To(
		BeEmpty(),
		"%d containers failed to be destroyed!",
		len(destroyContainerErrors),
	)
})

func CompileTestedExecutables() world.BuiltExecutables {
	var err error

	builtExecutables := world.BuiltExecutables{}

	builtExecutables["vizzini"] = compileVizzini("code.cloudfoundry.org/vizzini")

	builtExecutables["garden"], err = gexec.BuildIn(os.Getenv("GARDEN_GOPATH"), "code.cloudfoundry.org/guardian/cmd/gdn", "-race", "-a", "-tags", "daemon")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["auctioneer"], err = gexec.BuildIn(os.Getenv("AUCTIONEER_GOPATH"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["rep"], err = gexec.BuildIn(os.Getenv("REP_GOPATH"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["bbs"], err = gexec.BuildIn(os.Getenv("BBS_GOPATH"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["locket"], err = gexec.BuildIn(os.Getenv("LOCKET_GOPATH"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["file-server"], err = gexec.BuildIn(os.Getenv("FILE_SERVER_GOPATH"), "code.cloudfoundry.org/fileserver/cmd/file-server", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["route-emitter"], err = gexec.BuildIn(os.Getenv("ROUTE_EMITTER_GOPATH"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["router"], err = gexec.BuildIn(os.Getenv("ROUTER_GOPATH"), "code.cloudfoundry.org/gorouter", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["routing-api"], err = gexec.BuildIn(os.Getenv("ROUTING_API_GOPATH"), "code.cloudfoundry.org/routing-api/cmd/routing-api", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["ssh-proxy"], err = gexec.Build("code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")
	Expect(err).NotTo(HaveOccurred())

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"], err = gexec.Build("code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")
	Expect(err).NotTo(HaveOccurred())

	return builtExecutables
}

func compileVizzini(packagePath string, args ...string) string {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "vizzini")
	Expect(err).NotTo(HaveOccurred())

	executable := filepath.Join(tmpDir, path.Base(packagePath))

	cmdArgs := append([]string{"test"}, args...)
	cmdArgs = append(cmdArgs, "-c", "-o", executable, packagePath)

	build := exec.Command("go", cmdArgs...)
	build.Env = os.Environ()

	output, err := build.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to build vizzini:\n\nOutput:\n%s", string(output)))

	return executable
}

func CompileHealthcheckExecutable() string {
	healthcheckDir := world.TempDir("healthcheck")
	healthcheckPath, err := gexec.Build("code.cloudfoundry.org/healthcheck/cmd/healthcheck", "-race")
	Expect(err).NotTo(HaveOccurred())

	err = os.Rename(healthcheckPath, filepath.Join(healthcheckDir, "healthcheck"))
	Expect(err).NotTo(HaveOccurred())

	return healthcheckDir
}

func CompileLdsListenerExecutable() string {
	envoyPath := os.Getenv("ENVOY_PATH")

	ldsListenerPath, err := gexec.Build("code.cloudfoundry.org/rep/lds/cmd/lds", "-race")
	Expect(err).NotTo(HaveOccurred())

	err = os.Rename(ldsListenerPath, filepath.Join(envoyPath, "lds"))
	Expect(err).NotTo(HaveOccurred())

	return envoyPath
}
