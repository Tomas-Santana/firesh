// Package auth initialises a Firestore client using Google Application Default
// Credentials (ADC). No service-account key file is required; the standard
// credential chain is used:
//
//  1. GOOGLE_APPLICATION_CREDENTIALS env-var (path to a JSON key file)
//  2. gcloud application-default credentials  (~/.config/gcloud/…)
//  3. Attached service account (when running on GCP)
package auth

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"
)

// NewClient creates a Firestore client for the given project and database using ADC.
func NewClient(ctx context.Context, projectID, databaseID string) (*firestore.Client, error) {
	opts := []option.ClientOption{}

	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID, opts...)
	if err != nil {
		return nil, fmt.Errorf("firestore: %w\n\nEnsure ADC is configured:\n  gcloud auth application-default login\nor set GOOGLE_APPLICATION_CREDENTIALS to a service account key file", err)
	}
	return client, nil
}
