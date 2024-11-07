// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build integration_k8s

package k8s

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/siderolabs/talos/internal/integration/base"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// UserNamespaceSuite verifies that a pod with user namespace works.
type UserNamespaceSuite struct {
	base.K8sSuite
}

//go:embed testdata/usernamespace.yaml
var userNamespacePodSpec []byte

// SuiteName returns the name of the suite.
func (suite *UserNamespaceSuite) SuiteName() string {
	return "k8s.UserNamespaceSuite"
}

// TestUserNamespace verifies that a pod with user namespace works.
//
//nolint:gocyclo
func (suite *UserNamespaceSuite) TestUserNamespace() {
	if suite.Cluster == nil {
		suite.T().Skip("without full cluster state reaching out to the node IP is not reliable")
	}

	if suite.Cluster.Provisioner() != base.ProvisionerQEMU {
		suite.T().Skip("skipping usernamespace test since provisioner is not qemu")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	suite.T().Cleanup(cancel)

	node := suite.RandomDiscoveredNodeInternalIP(machine.TypeWorker)

	nodeCtx := client.WithNodes(ctx, node)

	reader, err := suite.Client.Read(nodeCtx, "/proc/sys/user/max_user_namespaces")
	suite.Require().NoError(err)

	var maxUserNamespaces bytes.Buffer

	_, err = maxUserNamespaces.ReadFrom(reader)
	suite.Require().NoError(err)

	if strings.TrimSpace(maxUserNamespaces.String()) == "0" {
		suite.T().Skip("skipping test since user namespace is disabled")
	}

	usernamespacePodManifest := suite.ParseManifests(userNamespacePodSpec)

	suite.T().Cleanup(func() {
		cleanUpCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()

		suite.DeleteManifests(cleanUpCtx, usernamespacePodManifest)
	})

	suite.ApplyManifests(ctx, usernamespacePodManifest)

	suite.Require().NoError(suite.WaitForPodToBeRunning(ctx, time.Minute, "default", "userns"))

	processResp, err := suite.Client.Processes(nodeCtx)
	suite.Require().NoError(err)

	var sleepProcessPID int

	for _, processInfo := range processResp.Messages {
		for _, process := range processInfo.Processes {
			if strings.Contains(process.Args, "sleep 1000") {
				sleepProcessPID = int(process.Pid)

				break
			}
		}
	}

	suite.Require().NotZero(sleepProcessPID, "sleep process not found for user namespace test")

	reader, err = suite.Client.Read(nodeCtx, fmt.Sprintf("/proc/%d/status", sleepProcessPID))
	suite.Require().NoError(err)

	var processStatus bytes.Buffer

	_, err = processStatus.ReadFrom(reader)
	suite.Require().NoError(err)

	scanner := bufio.NewScanner(&processStatus)

	var processUsingUserNamespace bool

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)

			if fields[0] != "0" && fields[1] != "0" && fields[2] != "0" && fields[3] != "0" {
				processUsingUserNamespace = true
			}

			break
		}
	}

	suite.Require().True(processUsingUserNamespace, "sleep process should not have root UID in host namespace\n", processStatus.String())
}

func init() {
	allSuites = append(allSuites, new(UserNamespaceSuite))
}
