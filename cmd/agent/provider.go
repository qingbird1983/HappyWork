package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/model/deepseek"
	"github.com/happywork/happywork/internal/model/mock"
)

// buildProvider selects the model backend. The API key is read only from the
// environment and never logged (ADR-0008, standards/06).
func buildProvider(choice, mockScript string, lg *log.Logger) (prov model.Provider, providerName, modelName string, isMock bool, err error) {
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice == "" {
		choice = "auto"
	}
	switch choice {
	case "auto":
		if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) != "" {
			choice = "deepseek"
		} else {
			choice = "mock"
		}
	case "mock", "deepseek":
	default:
		return nil, "", "", false, fmt.Errorf("provider must be auto, mock or deepseek (got %q)", choice)
	}
	if choice == "mock" {
		p, err := buildMock(mockScript)
		if err != nil {
			return nil, "", "", false, err
		}
		lg.Printf("provider=mock (offline; no network)")
		return p, "mock", "mock", true, nil
	}

	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		return nil, "", "", false, errors.New("provider deepseek requires DEEPSEEK_API_KEY in the environment")
	}
	base := os.Getenv("HAPPYWORK_DEEPSEEK_BASE_URL")
	name := os.Getenv("HAPPYWORK_DEEPSEEK_MODEL")
	thinking := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HAPPYWORK_DEEPSEEK_THINKING"))) {
	case "1", "true", "yes", "on":
		thinking = true
	}
	client, err := deepseek.New(deepseek.Config{APIKey: key, BaseURL: base, Model: name, Thinking: thinking})
	if err != nil {
		return nil, "", "", false, err
	}
	lg.Printf("provider=deepseek model=%s (key configured; value never logged)", client.Model())
	return client, "deepseek", client.Model(), false, nil
}

// buildMock loads embedded scripts; an explicit flag forces one script.
func buildMock(script string) (model.Provider, error) {
	scripts, err := mock.Load()
	if err != nil {
		return nil, err
	}
	if script != "" {
		return mock.New(scripts, func(string, int) string { return script }), nil
	}
	return mock.New(scripts, nil), nil
}
