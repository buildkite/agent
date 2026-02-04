package main

import (
	"context"
	"fmt"
	"log"

	"github.com/buildkite/agent/v3/jobapi"
)

// This file gets built and commited to this repo, then used as part of the hooks integration test to ensure that the
// bootstrap can run binary hooks
func main() {
	c, err := jobapi.NewDefaultClient(context.TODO())
	if err != nil {
		log.Fatalf("error: %v", fmt.Errorf("creating job api client: %w", err))
	}

	_, err = c.EnvUpdate(context.TODO(), &jobapi.EnvUpdateRequest{
		Env: map[string]string{
			"OCEAN":    "Pacífico",
			"MOUNTAIN": "chimborazo",
		},
	})
	if err != nil {
		log.Fatalf("error: %v", fmt.Errorf("updating env: %w", err))
	}

	fmt.Println("hi there from golang 🌊")
}
