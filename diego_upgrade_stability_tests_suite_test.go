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

type TestConfig struct {
	BoshDirectorURL   string `json:"bosh_director_url"`
	BoshAdminUser     string `json:"bosh_admin_user"`
	BoshAdminPassword string `json:"bosh_admin_password"`

	DiegoReleasePath string `json:"diego_release_path"`
	CfReleasePath    string `json:"cf_release_path"`
}

const BOSH_DEPLOY_TIMEOUT = 10 * time.Minute
const COMMAND_TIMEOUT = 5 * time.Second

var config *TestConfig

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

	targetCmd := exec.Command("bosh", "target", config.BoshDirectorURL)
	sess, err := gexec.Start(targetCmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess, COMMAND_TIMEOUT).Should(gexec.Exit(0))

	loginCmd := exec.Command("bosh", "login", config.BoshAdminUser, config.BoshAdminPassword)
	sess, err = gexec.Start(loginCmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess, COMMAND_TIMEOUT).Should(gexec.Exit(0))
})
