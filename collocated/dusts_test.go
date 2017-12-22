package dusts_test

import (
	"os"
	"os/exec"
	"path/filepath"

	"code.cloudfoundry.org/localip"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dusts", func() {
	It("runs vizzini succesfully", func() {
		ip, err := localip.LocalIP()
		Expect(err).NotTo(HaveOccurred())
		// routerHost, routerPort, err := net.SplitHostPort(componentMaker.Addresses.Router)
		// Expect(err).NotTo(HaveOccurred())
		flags := []string{
			"-nodes", "2", // run ginkgo in parallel
			"-randomizeAllSpecs",
			"-slowSpecThreshold", "10", // set threshold to 10s
			filepath.Join(os.Getenv("GOPATH"), "src/code.cloudfoundry.org/vizzini"),
			"--",
			"-bbs-address", "https://" + componentMaker.Addresses.BBS,
			"-bbs-client-cert", componentMaker.BbsSSL.ClientCert,
			"-bbs-client-key", componentMaker.BbsSSL.ClientKey,
			"-ssh-address", componentMaker.Addresses.SSHProxy,
			"-ssh-password", "",
			"-routable-domain-suffix", localIP + ".xip.io",
			"-host-address", ip,
		}

		cmd := exec.Command("ginkgo", flags...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		Expect(err).NotTo(HaveOccurred())
	})
})
