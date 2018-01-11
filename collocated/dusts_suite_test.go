package dusts_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/serviceclient"
	"code.cloudfoundry.org/consuladapter/consulrunner"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

var (
	ComponentMakerV0, ComponentMakerV1 world.ComponentMaker

	bbsClient        bbs.InternalClient
	bbsServiceClient serviceclient.ServiceClient
	gardenClient     garden.Client
	logger           lager.Logger
)

func TestDusts(t *testing.T) {
	helpers.RegisterDefaultTimeouts()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dusts Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	artifacts := make(map[string]world.BuiltArtifacts)
	oldArtifacts := world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	oldArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	oldArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	oldArtifacts.Executables = CompileTestedExecutablesV0()
	oldArtifacts.Healthcheck = CompileHealthcheckExecutableV0()
	artifacts["old"] = oldArtifacts

	newArtifacts := world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	newArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	newArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	newArtifacts.Executables = CompileTestedExecutablesV1()
	newArtifacts.Healthcheck = CompileHealthcheckExecutableV1()
	artifacts["new"] = newArtifacts

	payload, err := json.Marshal(artifacts)
	Expect(err).NotTo(HaveOccurred())

	return payload
}, func(payload []byte) {
	var artifacts map[string]world.BuiltArtifacts

	err := json.Unmarshal(payload, &artifacts)
	Expect(err).NotTo(HaveOccurred())

	_, dbBaseConnectionString := world.DBInfo()

	// TODO: the hard coded addresses for router and file server prevent running multiple dusts tests at the same time
	addresses := world.ComponentAddresses{
		GardenLinux:         fmt.Sprintf("127.0.0.1:%d", 10000+config.GinkgoConfig.ParallelNode),
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
		LocalNodePlugin:     fmt.Sprintf("127.0.0.1:%d", 25550+config.GinkgoConfig.ParallelNode),
		Locket:              fmt.Sprintf("127.0.0.1:%d", 26500+config.GinkgoConfig.ParallelNode),
		SQL:                 fmt.Sprintf("%sdiego_%d", dbBaseConnectionString, config.GinkgoConfig.ParallelNode),
	}

	ComponentMakerV0 = world.MakeV0ComponentMaker("fixtures/certs/", artifacts["old"], addresses)
	ComponentMakerV0.Setup()

	ComponentMakerV1 = world.MakeComponentMaker("fixtures/certs/", artifacts["new"], addresses)
	ComponentMakerV1.Setup()
})

var _ = AfterSuite(func() {
	ComponentMakerV0.Teardown()
})

func CompileTestedExecutablesV1() world.BuiltExecutables {
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

	builtExecutables["ssh-proxy"], err = gexec.BuildIn(os.Getenv("SSH_PROXY_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")
	Expect(err).NotTo(HaveOccurred())

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"], err = gexec.BuildIn(os.Getenv("SSHD_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")
	Expect(err).NotTo(HaveOccurred())

	return builtExecutables
}

func CompileTestedExecutablesV0() world.BuiltExecutables {
	var err error

	builtExecutables := world.BuiltExecutables{}

	// compiling v0 version of diego components
	builtExecutables["auctioneer"], err = gexec.BuildIn(os.Getenv("AUCTIONEER_GOPATH_V0"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["rep"], err = gexec.BuildIn(os.Getenv("REP_GOPATH_V0"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["bbs"], err = gexec.BuildIn(os.Getenv("BBS_GOPATH_V0"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	Expect(err).NotTo(HaveOccurred())

	builtExecutables["route-emitter"], err = gexec.BuildIn(os.Getenv("ROUTE_EMITTER_GOPATH_V0"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	Expect(err).NotTo(HaveOccurred())

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"], err = gexec.BuildIn(os.Getenv("SSHD_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
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

func CompileHealthcheckExecutableV0() string {
	healthcheckDir := world.TempDir("healthcheck")
	healthcheckPath, err := gexec.BuildIn(os.Getenv("HEALTHCHECK_GOPATH_V0"), "code.cloudfoundry.org/healthcheck/cmd/healthcheck", "-race")
	Expect(err).NotTo(HaveOccurred())

	err = os.Rename(healthcheckPath, filepath.Join(healthcheckDir, "healthcheck"))
	Expect(err).NotTo(HaveOccurred())

	return healthcheckDir
}

func CompileHealthcheckExecutableV1() string {
	healthcheckDir := world.TempDir("healthcheck")
	healthcheckPath, err := gexec.BuildIn(os.Getenv("HEALTHCHECK_GOPATH"), "code.cloudfoundry.org/healthcheck/cmd/healthcheck", "-race")
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
