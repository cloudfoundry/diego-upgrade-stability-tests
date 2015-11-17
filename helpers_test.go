package upgrade_test

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/generator"
	"github.com/cloudfoundry-incubator/cf-test-helpers/runner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

const (
	CF_API            = "https://api.bosh-lite.com"
	CF_USER           = "admin"
	CF_PASSWORD       = "admin"
	APP_ROUTE_PATTERN = "http://%s.bosh-lite.com/"
)

var testApp *cfApp

func boshCmd(manifest, action, completeMsg string) {
	args := []string{"-n"}
	if manifest != "" {
		args = append(args, "-d", manifest)
	}
	args = append(args, strings.Split(action, " ")...)
	cmd := bosh(args...)
	sess, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess, BOSH_DEPLOY_TIMEOUT).Should(gexec.Exit(0))
	Expect(sess).To(gbytes.Say(completeMsg))
}

func guidForAppName(appName string) string {
	cfApp := cf.Cf("app", appName, "--guid")
	Expect(cfApp.Wait()).To(gexec.Exit(0))

	appGuid := strings.TrimSpace(string(cfApp.Out.Contents()))
	Expect(appGuid).NotTo(Equal(""))
	return appGuid
}

type cfApp struct {
	appName, appRoute, orgName, spaceName string
}

func newCfApp() *cfApp {
	appName := generator.RandomName()
	return &cfApp{
		appName:   appName,
		appRoute:  fmt.Sprintf(APP_ROUTE_PATTERN, appName),
		orgName:   generator.RandomName(),
		spaceName: generator.RandomName(),
	}
}

func (a *cfApp) push() {
	setup(a.orgName, a.spaceName)
	Eventually(cf.Cf("push", a.appName, "-p", "dora", "-i", "1"), 5*time.Minute).Should(gexec.Exit(0))
	Eventually(cf.Cf("logs", a.appName, "--recent")).Should(gbytes.Say("[HEALTH/0]"))
	curlAppMain := func() string {
		return a.curl("")
	}

	Eventually(curlAppMain).Should(ContainSubstring("Hi, I'm Dora!"))
}

func (a *cfApp) curl(endpoint string) string {
	curlCmd := runner.Curl(a.appRoute + endpoint)
	runner.NewCmdRunner(curlCmd, 30*time.Second).Run()
	Expect(string(curlCmd.Err.Contents())).To(HaveLen(0))
	return string(curlCmd.Out.Contents())
}

func (a *cfApp) scale(numInstances int) {
	Eventually(cf.Cf("scale", a.appName, "-i", strconv.Itoa(numInstances))).Should(gexec.Exit(0))
	found := make(map[string]struct{})
	for i := 0; i < numInstances*5; i++ {
		id := a.curl("id")
		found[id] = struct{}{}
	}
	Expect(found).To(HaveLen(numInstances))
}

func (a *cfApp) destroy() {
	Eventually(cf.Cf("delete", "-r", "-f", a.appName)).Should(gexec.Exit(0))
	teardownOrg(a.orgName)
}

func setup(org, space string) {
	Eventually(cf.Cf("login", "-a", CF_API, "-u", CF_USER, "-p", CF_PASSWORD, "--skip-ssl-validation")).Should(gexec.Exit(0))
	Eventually(cf.Cf("create-org", org)).Should(gexec.Exit(0))
	Eventually(cf.Cf("target", "-o", org)).Should(gexec.Exit(0))
	Eventually(cf.Cf("create-space", space)).Should(gexec.Exit(0))
	Eventually(cf.Cf("target", "-s", space)).Should(gexec.Exit(0))
}

func teardownOrg(orgName string) {
	Eventually(cf.Cf("delete-org", "-f", orgName)).Should(gexec.Exit(0))
}

func smokeTestDiego() {
	smokeTestApp := newCfApp()
	smokeTestApp.push()
	defer smokeTestApp.destroy()

	Eventually(cf.Cf("cf", "ssh", "dora", "-c", `"cat app/Gemfile"`)).Should(gexec.Exit(0))
}

func deployTestApp() {
	testApp = newCfApp()
	testApp.push()
}

func scaleTestApp(numInstances int) {
	testApp.scale(numInstances)
}
