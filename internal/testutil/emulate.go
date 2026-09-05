package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

const (
	emulateImage          = "node:22-alpine"
	emulatePackageVersion = "0.8.0"
	emulateGitHubPort     = "4001/tcp"
	emulateGooglePort     = "4002/tcp"
	emulateApplePort      = "4004/tcp"
	emulateMicrosoftPort  = "4005/tcp"
	emulateOktaPort       = "4006/tcp"
)

type EmulateContainer struct {
	Container        testcontainers.Container
	GitHubBaseURL    string
	GoogleBaseURL    string
	AppleBaseURL     string
	MicrosoftBaseURL string
	OktaBaseURL      string
}

var (
	emulateContainerOnce sync.Once
	emulateContainer     testcontainers.Container
	emulateContainerErr  error
)

func startEmulateContainer() error {
	emulateContainerOnce.Do(func() {
		ctx := context.Background()

		emulateContainer, emulateContainerErr = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: emulateImage,
				ExposedPorts: []string{
					emulateGitHubPort,
					emulateGooglePort,
					emulateApplePort,
					emulateMicrosoftPort,
					emulateOktaPort,
				},
				Env: map[string]string{
					"NPM_CONFIG_UPDATE_NOTIFIER": "false",
				},
				Cmd: []string{
					"sh",
					"-c",
					fmt.Sprintf(
						"npm install -g emulate@%s && emulate start --port 4000",
						emulatePackageVersion,
					),
				},
				WaitingFor: wait.ForLog("google      http://localhost:4002").
					WithStartupTimeout(2 * time.Minute),
			},
			Started: true,
		})
		if emulateContainerErr != nil {
			return
		}
	})

	if emulateContainerErr != nil {
		return errors.Wrap(emulateContainerErr, "error starting emulate container")
	}
	return nil
}

func emulateBaseURL(ctx context.Context, port string) (string, error) {
	host, err := emulateContainer.Host(ctx)
	if err != nil {
		return "", errors.Wrap(err, "error getting emulate host")
	}

	mappedPort, err := emulateContainer.MappedPort(ctx, port)
	if err != nil {
		return "", errors.Wrap(err, "error getting emulate port")
	}

	return fmt.Sprintf("http://%s:%s", host, mappedPort.Port()), nil
}

// SetupEmulateContainer returns a shared Emulate container for OAuth services.
// The container is started once per test binary and reused across tests.
func SetupEmulateContainer(t *testing.T) *EmulateContainer {
	t.Helper()

	require.NoError(t, startEmulateContainer())

	ctx := context.Background()
	githubBaseURL, err := emulateBaseURL(ctx, emulateGitHubPort)
	require.NoError(t, err)
	googleBaseURL, err := emulateBaseURL(ctx, emulateGooglePort)
	require.NoError(t, err)
	appleBaseURL, err := emulateBaseURL(ctx, emulateApplePort)
	require.NoError(t, err)
	microsoftBaseURL, err := emulateBaseURL(ctx, emulateMicrosoftPort)
	require.NoError(t, err)
	oktaBaseURL, err := emulateBaseURL(ctx, emulateOktaPort)
	require.NoError(t, err)

	return &EmulateContainer{
		Container:        emulateContainer,
		GitHubBaseURL:    githubBaseURL,
		GoogleBaseURL:    googleBaseURL,
		AppleBaseURL:     appleBaseURL,
		MicrosoftBaseURL: microsoftBaseURL,
		OktaBaseURL:      oktaBaseURL,
	}
}
