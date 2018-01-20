package dusts_test

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/lager"

	. "github.com/onsi/ginkgo"
)

var nRetries = 10

type poller struct {
	logger     lager.Logger
	routerAddr string
	host       string
}

func NewPoller(logger lager.Logger, routerAddr, host string) *poller {
	return &poller{
		logger:     logger,
		routerAddr: routerAddr,
		host:       host,
	}
}

func (c *poller) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	defer GinkgoRecover()

loop:
	for {
		select {
		case <-signals:
			c.logger.Info("exiting-poller")
			return nil

		default:
			_, status, _ := helpers.ResponseBodyAndStatusCodeFromHost(c.routerAddr, c.host)

			if status == http.StatusOK {
				break loop
			}
		}
	}

	close(ready)

	for {
		select {
		case <-signals:
			c.logger.Info("exiting-poller")
			return nil

		default:
			status, err := c.pollWithRetries()
			if err != nil {
				return err
			}

			if status != http.StatusOK {
				return errors.New(fmt.Sprintf("request failed with status %d", status))
			}
		}
	}
}

func (c *poller) pollWithRetries() (int, error) {
	var status, retry int
	var err error

	for retry = 0; retry <= nRetries; retry++ {
		_, status, err = helpers.ResponseBodyAndStatusCodeFromHost(c.routerAddr, c.host)

		switch status {
		case http.StatusNotFound:
			c.logger.Info("poller-status-not-found", lager.Data{"status": status, "error": err, "retry": retry})
			continue
		default:
			c.logger.Info("poller-exit-status", lager.Data{"status": status, "error": err, "retry": retry})
			return status, err
		}
	}

	c.logger.Info("poller-no-more-retries", lager.Data{"status": status, "error": err, "retry": retry})
	return status, err
}
