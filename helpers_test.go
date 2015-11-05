package upgrade_test

import (
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

func boshCmd(manifest, action, completeMsg string) {
	args := []string{"-d", manifest, "-n"}
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

func enableDiego(appName string) {
	guid := guidForAppName(appName)
	Eventually(cf.Cf("curl", "/v2/apps/"+guid, "-X", "PUT", "-d", `{"diego": true}`)).Should(gexec.Exit(0))
}

func smokeTestDiego() {
	CFAPI := "https://api.bosh-lite.com"
	CFUser := "admin"
	CFPassword := "admin"
	appName := generator.RandomName()
	appsDomain := "bosh-lite.com"
	appRoute := "http://" + appName + "." + appsDomain + "/"
	orgName := generator.RandomName()
	spaceName := "smoke"

	Eventually(cf.Cf("login", "-a", CFAPI, "-u", CFUser, "-p", CFPassword, "--skip-ssl-validation")).Should(gexec.Exit(0))

	Eventually(cf.Cf("create-org", orgName)).Should(gexec.Exit(0))
	defer func() { Eventually(cf.Cf("delete-org", "-f", orgName)).Should(gexec.Exit(0)) }()
	Eventually(cf.Cf("target", "-o", orgName)).Should(gexec.Exit(0))

	Eventually(cf.Cf("create-space", spaceName)).Should(gexec.Exit(0))
	Eventually(cf.Cf("target", "-s", spaceName)).Should(gexec.Exit(0))

	Eventually(cf.Cf("push", appName, "-p", "dora", "--no-start", "-i", "2")).Should(gexec.Exit(0))
	defer func() { Eventually(cf.Cf("delete", "-r", "-f", appName)).Should(gexec.Exit(0)) }()
	enableDiego(appName)
	Eventually(cf.Cf("start", appName), 5*time.Minute).Should(gexec.Exit(0))
	Eventually(cf.Cf("logs", appName, "--recent")).Should(gbytes.Say("[HEALTH/0]"))

	curlAppRouteWithTimeout := func() string {
		curlCmd := runner.Curl(appRoute)
		runner.NewCmdRunner(curlCmd, 30*time.Second).Run()
		Expect(string(curlCmd.Err.Contents())).To(HaveLen(0))
		return string(curlCmd.Out.Contents())
	}
	Eventually(curlAppRouteWithTimeout).Should(ContainSubstring("Hi, I'm Dora!"))
	Eventually(cf.Cf("delete", "-r", "-f", appName)).Should(gexec.Exit(0))
}
