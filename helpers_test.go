package upgrade_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
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

func newCfApp(appNamePrefix string) *cfApp {
	appName := appNamePrefix + "-" + generator.RandomName()
	return &cfApp{
		appName:   appName,
		appRoute:  fmt.Sprintf(APP_ROUTE_PATTERN, appName),
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

func (a *cfApp) curl(endpoint string) (string, error) {
	log.Printf("Curling endpoint [%s]", a.appRoute+endpoint)
	resp, err := http.Get(a.appRoute + endpoint)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	log.Printf("Response [%d][%s]", resp.StatusCode, body)
	if resp.StatusCode != 200 {
		return "", errors.New("Status Code not 200")
	}
	return string(body), nil
}

func (a *cfApp) scale(numInstances int) {
	Eventually(cf.Cf("target", "-o", a.orgName, "-s", a.spaceName)).Should(gexec.Exit(0))
	Eventually(cf.Cf("scale", a.appName, "-i", strconv.Itoa(numInstances))).Should(gexec.Exit(0))
	found := make(map[string]struct{})
	for i := 0; i < numInstances*10; i++ {
		id, err := a.curl("id")
		Expect(err).NotTo(HaveOccurred())
		found[id] = struct{}{}
		time.Sleep(1 * time.Second)
	}
	Expect(found).To(HaveLen(numInstances))
}

func (a *cfApp) verifySsh() {
	envCmd := cf.Cf("ssh", a.appName, "-c", `"/usr/bin/env"`)
	Expect(envCmd.Wait()).To(gexec.Exit(0))

	output := string(envCmd.Buffer().Contents())

	Expect(string(output)).To(MatchRegexp(fmt.Sprintf(`VCAP_APPLICATION=.*"application_name":"%s"`, a.appName)))
	Expect(string(output)).To(MatchRegexp("INSTANCE_INDEX=0"))

	Eventually(cf.Cf("logs", a.appName, "--recent")).Should(gbytes.Say("Successful remote access"))
	Eventually(cf.Cf("events", a.appName)).Should(gbytes.Say("audit.app.ssh-authorized"))
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
	smokeTestApp := newCfApp("smoke-test")
	smokeTestApp.push()
	defer smokeTestApp.destroy()
	smokeTestApp.verifySsh()
}

func deployTestApp() {
	testApp = newCfApp("test-app")
	testApp.push()
}

func performPollTestApp() ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		defer GinkgoRecover()

		curlComplete := make(chan error, 1)
		curlTimer := time.NewTimer(0)

		close(ready)

		for {
			select {
			case <-curlTimer.C:
				go func() {
					_, err := testApp.curl("id")
					if err != nil {
					}
					curlComplete <- err
				}()

			case err := <-curlComplete:
				if err != nil {
					Fail("Polling Test App Failed")
				}
				curlTimer.Reset(2 * time.Second)

			case <-signals:
				return nil
			}
		}
	})
}

func teardownTestOrg() {
	teardownOrg(testApp.orgName)
}

func scaleTestApp(numInstances int) {
	testApp.scale(numInstances)
}
