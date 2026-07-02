// Package secrets reads secret values from Google Secret Manager.
package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// Get retrieves a secret value by name. name may be a full resource path or a
// short secret ID (then GOOGLE_CLOUD_PROJECT/GCP_PROJECT_ID + latest version is used).
func Get(ctx context.Context, name string) (string, error) {
	full := name
	if !strings.Contains(name, "/") {
		pid := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
		if pid == "" {
			pid = strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
		}
		if pid == "" {
			return "", fmt.Errorf("secrets: GOOGLE_CLOUD_PROJECT not set for short secret name %q", name)
		}
		full = fmt.Sprintf("projects/%s/secrets/%s/versions/latest", pid, name)
	}
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	res, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: full})
	if err != nil {
		return "", err
	}
	if res.Payload == nil || res.Payload.Data == nil {
		return "", fmt.Errorf("secrets: empty payload for %q", name)
	}
	return string(res.Payload.Data), nil
}
