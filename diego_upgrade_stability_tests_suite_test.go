package upgrade_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

const BOSH_DEPLOY_TIMEOUT = 10 * time.Minute
const COMMAND_TIMEOUT = 30 * time.Second

var config *TestConfig

type TestConfig struct {
	BoshDirectorURL   string `json:"bosh_director_url"`
	BoshAdminUser     string `json:"bosh_admin_user"`
	BoshAdminPassword string `json:"bosh_admin_password"`

	DiegoReleasePath string `json:"diego_release_path"`
	CfReleasePath    string `json:"cf_release_path"`
}

func bosh(args ...string) *exec.Cmd {
	boshArgs := append([]string{"-t", config.BoshDirectorURL, "-u", config.BoshAdminUser, "-p", config.BoshAdminPassword}, args...)
	return exec.Command("bosh", boshArgs...)
}

func TestUpgradeStableManifests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UpgradeStableManifests Suite")
}

var _ = BeforeSuite(func() {
	config = &TestConfig{}

	path := os.Getenv("CONFIG")
	Expect(path).NotTo(Equal(""))

	configFile, err := os.Open(path)
	Expect(err).NotTo(HaveOccurred())

	decoder := json.NewDecoder(configFile)
	err = decoder.Decode(config)
	Expect(err).NotTo(HaveOccurred())

	boshTargetCmd := bosh("target", config.BoshDirectorURL)
	sess, err := gexec.Start(boshTargetCmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess, COMMAND_TIMEOUT).Should(gexec.Exit(0))
})
