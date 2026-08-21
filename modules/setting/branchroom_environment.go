// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	branchroomDevelopmentFileProvider = "development-file"
	branchroomDefaultRevisionBytes    = 64 * 1024
	branchroomMaxRevisionBytes        = 1024 * 1024
	branchroomDefaultLeaseDuration    = 5 * time.Minute
	branchroomMinLeaseDuration        = time.Second
	branchroomMaxLeaseDuration        = time.Hour
)

// BranchroomEnvironmentConfig contains development-only encrypted-environment settings.
// The key file path is safe configuration metadata; its key material is never retained here.
type BranchroomEnvironmentConfig struct {
	Enabled                bool
	Provider               string
	DevelopmentKeyFilePath string
	MaxRevisionBytes       int
	LeaseDuration          time.Duration
}

// BranchroomEnvironment stores the static Branchroom encrypted-environment configuration.
var BranchroomEnvironment BranchroomEnvironmentConfig

func loadBranchroomEnvironmentFrom(rootCfg ConfigProvider) error {
	sec := rootCfg.Section("branchroom.environment")
	BranchroomEnvironment = BranchroomEnvironmentConfig{}
	if !Branchroom.Enabled || !sec.Key("ENABLED").MustBool(false) {
		return nil
	}
	if IsProd {
		return errors.New("[branchroom.environment] development encryption is unavailable in production")
	}

	provider := strings.TrimSpace(sec.Key("PROVIDER").MustString(branchroomDevelopmentFileProvider))
	if provider != branchroomDevelopmentFileProvider {
		return fmt.Errorf("[branchroom.environment].PROVIDER must be %q", branchroomDevelopmentFileProvider)
	}
	keyFilePath, err := resolveBranchroomEnvironmentKeyFile(sec.Key("DEVELOPMENT_KEY_URI").String())
	if err != nil {
		return err
	}

	maxRevisionBytes := sec.Key("MAX_REVISION_BYTES").MustInt(branchroomDefaultRevisionBytes)
	if maxRevisionBytes <= 0 || maxRevisionBytes > branchroomMaxRevisionBytes {
		return fmt.Errorf("[branchroom.environment].MAX_REVISION_BYTES must be between 1 and %d", branchroomMaxRevisionBytes)
	}
	leaseDuration := sec.Key("LEASE_DURATION").MustDuration(branchroomDefaultLeaseDuration)
	if leaseDuration < branchroomMinLeaseDuration || leaseDuration > branchroomMaxLeaseDuration {
		return fmt.Errorf("[branchroom.environment].LEASE_DURATION must be between %s and %s", branchroomMinLeaseDuration, branchroomMaxLeaseDuration)
	}

	BranchroomEnvironment = BranchroomEnvironmentConfig{
		Enabled:                true,
		Provider:               provider,
		DevelopmentKeyFilePath: keyFilePath,
		MaxRevisionBytes:       maxRevisionBytes,
		LeaseDuration:          leaseDuration,
	}
	return nil
}

func resolveBranchroomEnvironmentKeyFile(rawURI string) (string, error) {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return "", errors.New("[branchroom.environment].DEVELOPMENT_KEY_URI must be a file URI")
	}
	uri, err := url.Parse(rawURI)
	if err != nil || uri.Scheme != "file" || uri.Host != "" || uri.RawQuery != "" || uri.Fragment != "" || uri.Path == "" || !filepath.IsAbs(uri.Path) {
		return "", errors.New("[branchroom.environment].DEVELOPMENT_KEY_URI must be an absolute file URI")
	}
	path := filepath.Clean(uri.Path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("[branchroom.environment].DEVELOPMENT_KEY_URI must name an existing file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("[branchroom.environment].DEVELOPMENT_KEY_URI must name a regular file")
	}
	return path, nil
}
