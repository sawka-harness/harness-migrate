// Copyright © 2026 Harness Inc.
// SPDX-License-Identifier: Apache-2.0

package migrateplugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/harness/cli/pkg/auth"
	"github.com/harness/cli/pkg/cmdctx"
	"github.com/harness/cli/pkg/hlog"

	"github.com/harness/harness-migrate/internal/gitexporter"
	"github.com/harness/harness-migrate/internal/gitimporter"
	"github.com/harness/harness-migrate/internal/report"
	"github.com/harness/harness-migrate/plugin/pkg/bridge"
)

const migrateBundleToRepositoryID = "migrate_scm_bundle_repository"

// migrateBundleToRepository creates Harness Code repositories from a bundle an
// export wrote. It calls the same engine constructor as the standalone
// `harness-migrate git-import` command; the endpoint, token and destination
// space come from the host's resolved profile instead of from flags.
func migrateBundleToRepository(ctx *cmdctx.Ctx) error {
	// --from is declared presence: required in the spec, so it is non-empty
	// here. --to is presence: none — the destination is the profile's scope.
	zipPath, err := resolveBundleZip(ctx.MigrateFrom)
	if err != nil {
		return err
	}
	space, err := harnessSpace(ctx.Auth)
	if err != nil {
		return err
	}
	// harness.Client authenticates with an x-api-key header, which only a PAT or
	// SAT can fill; an SSO session's bearer token is not interchangeable. Test
	// the token rather than AuthType, which is left unset in HARNESS_API_KEY mode.
	if ctx.Auth.PATToken == "" {
		return errors.New("importing needs an API token: the migration engine authenticates with x-api-key, " +
			"which an SSO session cannot provide. Run 'harness auth login' with an API token (without --sso), " +
			"or set HARNESS_API_KEY.")
	}

	repository := strings.Trim(cmdctx.GetString(ctx.FlagValues, "repo"), "/")
	fileSizeLimit, err := int64Flag(ctx.FlagValues, "file-size-limit")
	if err != nil {
		return err
	}
	batchSize, err := int64Flag(ctx.FlagValues, "batch-size")
	if err != nil {
		return err
	}

	flags := gitimporter.Flags{
		SkipUsers:     cmdctx.GetBool(ctx.FlagValues, "skip-users"),
		FileSizeLimit: fileSizeLimit,
		NoPR:          cmdctx.GetBool(ctx.FlagValues, "no-pr"),
		NoWebhook:     cmdctx.GetBool(ctx.FlagValues, "no-webhook"),
		NoRule:        cmdctx.GetBool(ctx.FlagValues, "no-rule"),
		NoLabel:       cmdctx.GetBool(ctx.FlagValues, "no-label"),
		NoGit:         cmdctx.GetBool(ctx.FlagValues, "no-git"),
		PRBatchSize:   int(batchSize),
	}

	tracer := bridge.NewTracer(cmdctx.GetBool(ctx.FlagValues, "no-progress"))
	defer tracer.Close()

	requestID := uuid.New().String()
	endpoint, _ := strings.CutSuffix(ctx.Auth.APIUrl, "/")
	reporter := make(map[string]*report.Report)

	importer := gitimporter.NewImporter(
		endpoint, space, repository, ctx.Auth.PATToken, zipPath,
		requestID, false, cmdctx.GetBool(ctx.FlagValues, "trace"),
		flags, tracer, reporter,
	)

	bgCtx, cancel := bridge.WithInterrupt(ctx.Context)
	defer cancel()

	hlog.Debug("starting scm_bundle:repository migration", "zip", zipPath, "space", space, "repository", repository)
	tracer.Log("importing %s into %s with id: %s", zipPath, space, requestID)
	return importer.Import(bgCtx)
}

// resolveBundleZip turns --from into the zip path the importer reads.
//
// A file is the zip itself, which is all the standalone CLI accepted. A folder is
// an export's output folder, and the zip inside it is found with the same rule the
// export used to write it — an export leaves nothing else behind, so
// `--from harness` mirrors the export's `--to harness`. Either way the result is
// opened before the import announces itself, so a wrong path fails naming the
// path rather than as "not a valid zip file" from deep in the engine.
func resolveBundleZip(from string) (string, error) {
	path := filepath.Clean(from)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read bundle %q: %w", from, err)
	}
	if info.IsDir() {
		path = gitexporter.ZipFilePath(path)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("folder %q holds no %s; point --from at the zip file itself if it goes by another name: %w",
				from, gitexporter.ZipFileName, err)
		}
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("%q is not a readable zip file: %w", path, err)
	}
	_ = zr.Close()
	return path, nil
}

// harnessSpace renders the resolved scope as the account/org/project path the
// importer creates repositories under — the value the standalone CLI takes as
// --space. Code repositories are multi-level, so an org or account scope is
// legitimate; only a project without an org is not.
func harnessSpace(a *auth.ResolvedAuth) (string, error) {
	if a.AccountID == "" {
		return "", errors.New("the resolved profile has no account; run 'harness auth login'")
	}
	if a.ProjectID != "" && a.OrgID == "" {
		return "", errors.New("a project scope needs an org too; pass --org or run 'harness auth setscope'")
	}
	parts := []string{a.AccountID}
	if a.OrgID != "" {
		parts = append(parts, a.OrgID)
	}
	if a.ProjectID != "" {
		parts = append(parts, a.ProjectID)
	}
	return strings.Join(parts, "/"), nil
}

// int64Flag parses a numeric flag. Spec flags are only ever strings or bools, so
// the conversion lives here rather than being silently swallowed by
// cmdctx.GetInt — a typo'd batch size should not turn into zero.
func int64Flag(fv map[string]any, name string) (int64, error) {
	raw := cmdctx.GetString(fv, name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("--%s must be a whole number, got %q", name, raw)
	}
	return n, nil
}
