package upgrade_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/generator"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
)

const (
	CF_USER           = "admin"
	CF_PASSWORD       = "admin"
	APP_ROUTE_PATTERN = "http://%s.%s"
)

var (
	testApp *cfApp
)

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
	appName, orgName, spaceName string
	appRoute                    url.URL
}

func newCfApp(appNamePrefix string) *cfApp {
	appName := appNamePrefix + "-" + generator.RandomName()
	rawUrl := fmt.Sprintf(APP_ROUTE_PATTERN, appName, config.OverrideDomain)
	appUrl, err := url.Parse(rawUrl)
	if err != nil {
		panic(err)
	}
	return &cfApp{
		appName:   appName,
		appRoute:  *appUrl,
		orgName:   "org-" + generator.RandomName(),
		spaceName: "space-" + generator.RandomName(),
	}
}

func (a *cfApp) push() {
	setup(a.orgName, a.spaceName)
	Eventually(cf.Cf("push", a.appName, "-p", "dora", "-i", "1", "-b", "ruby_buildpack"), 5*time.Minute).Should(gexec.Exit(0))
	Eventually(cf.Cf("logs", a.appName, "--recent")).Should(gbytes.Say("[HEALTH/0]"))
	curlAppMain := func() string {
		response, err := a.curl("")
		if err != nil {
			return ""
		}
		return response
	}

	Eventually(curlAppMain).Should(ContainSubstring("Hi, I'm Dora!"))
}

func curl(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Endpoint: %s, Status Code: %d, Body: %s", url, resp.StatusCode, string(body))
	}
	return string(body), nil
}

func (a *cfApp) curl(endpoint string) (string, error) {
	endpointUrl := a.appRoute
	endpointUrl.Path = endpoint
	return curl(endpointUrl.String())
}

func (a *cfApp) scale(numInstances int) {
	Eventually(cf.Cf("target", "-o", a.orgName, "-s", a.spaceName)).Should(gexec.Exit(0))
	Eventually(cf.Cf("scale", a.appName, "-i", strconv.Itoa(numInstances))).Should(gexec.Exit(0))
	Eventually(func() int {
		found := make(map[string]struct{})
		for i := 0; i < numInstances*2; i++ {
			id, err := a.curl("id")
			if err != nil {
				log.Printf("Failed Curling While Scaling: %s\n", err.Error())
				return -1
			}
			found[id] = struct{}{}
			time.Sleep(1 * time.Second)
		}
		return len(found)
	}).Should(Equal(numInstances))
}

func (a *cfApp) verifySsh(instanceIndex int) {
	envCmd := cf.Cf("ssh", a.appName, "-i", strconv.Itoa(instanceIndex), "-c", `"/usr/bin/env"`)
	Expect(envCmd.Wait()).To(gexec.Exit(0))

	output := string(envCmd.Buffer().Contents())

	Expect(string(output)).To(MatchRegexp(fmt.Sprintf(`VCAP_APPLICATION=.*"application_name":"%s"`, a.appName)))
	Expect(string(output)).To(MatchRegexp(fmt.Sprintf("INSTANCE_INDEX=%d", instanceIndex)))

	Eventually(cf.Cf("logs", a.appName, "--recent")).Should(gbytes.Say("Successful remote access"))
	Eventually(cf.Cf("events", a.appName)).Should(gbytes.Say("audit.app.ssh-authorized"))
}

func (a *cfApp) destroy() {
	Eventually(cf.Cf("delete", "-r", "-f", a.appName)).Should(gexec.Exit(0))
	teardownOrg(a.orgName)
}

func setup(org, space string) {
	Eventually(func() int {
		return cf.Cf("login", "-a", "api."+config.OverrideDomain, "-u", CF_USER, "-p", CF_PASSWORD, "--skip-ssl-validation").Wait().ExitCode()
	}).Should(Equal(0))
	Eventually(cf.Cf("create-org", org)).Should(gexec.Exit(0))
	Eventually(cf.Cf("target", "-o", org)).Should(gexec.Exit(0))
	Eventually(cf.Cf("create-space", space)).Should(gexec.Exit(0))
	Eventually(cf.Cf("target", "-s", space)).Should(gexec.Exit(0))
}

func teardownOrg(orgName string) {
	Eventually(cf.Cf("delete-org", "-f", orgName)).Should(gexec.Exit(0))
}

func smokeTestDiego() {
	smokeTestApp := newCfApp("smoke-test")
	// push new app
	smokeTestApp.push()

	// destroy after test finishes
	defer smokeTestApp.destroy()

	// verify scaling up
	smokeTestApp.scale(2)

	// verify ssh functionality
	smokeTestApp.verifySsh(0)
	smokeTestApp.verifySsh(1)

	// verify scaling down
	smokeTestApp.scale(1)
}

func deployTestApp() {
	testApp = newCfApp("test-app")
	testApp.push()
}

var pollTestApp ifrit.RunFunc = func(signals <-chan os.Signal, ready chan<- struct{}) error {
	defer GinkgoRecover()

	close(ready)

	curlTimer := time.NewTimer(0)
	for {
		select {
		case <-curlTimer.C:
			_, err := testApp.curl("id")
			Expect(err).NotTo(HaveOccurred(), "continuous polling failed")
			curlTimer.Reset(2 * time.Second)

		case <-signals:
			By("exiting continuous test poller")
			return nil
		}
	}
}

func teardownTestOrg() {
	teardownOrg(testApp.orgName)
}

func scaleTestApp(numInstances int) {
	testApp.scale(numInstances)
}
