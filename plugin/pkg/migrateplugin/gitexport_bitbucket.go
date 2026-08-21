// Copyright © 2026 Harness Inc.
// SPDX-License-Identifier: Apache-2.0

package migrateplugin

import (
	"fmt"
	"strings"

	"github.com/drone/go-scm/scm"
	scmbitbucket "github.com/drone/go-scm/scm/driver/bitbucket"

	"github.com/harness/cli/pkg/cmdctx"
	"github.com/harness/cli/pkg/hlog"

	"github.com/harness/harness-migrate/internal/checkpoint"
	"github.com/harness/harness-migrate/internal/gitexporter"
	"github.com/harness/harness-migrate/internal/migrate/bitbucket"
	"github.com/harness/harness-migrate/internal/report"
	"github.com/harness/harness-migrate/plugin/pkg/bridge"
)

const executeGitExportBitbucketID = "execute_git_export_bitbucket"

// executeGitExportBitbucket exports a Bitbucket Cloud workspace's git data to a
// local folder. It calls the same engine constructors as the standalone
// `harness-migrate bitbucket git-export` command; only the front-end differs.
func executeGitExportBitbucket(ctx *cmdctx.Ctx) error {
	dir := cmdctx.GetString(ctx.FlagValues, "dir")
	// bitbucket-workspace and bitbucket-token are declared required in the
	// spec, so cobra has already rejected the run if either is missing.
	workspace := strings.Trim(cmdctx.GetString(ctx.FlagValues, "bitbucket-workspace"), "/")
	token := cmdctx.GetString(ctx.FlagValues, "bitbucket-token")
	repository := strings.Trim(cmdctx.GetString(ctx.FlagValues, "bitbucket-repo"), "/")
	host := cmdctx.GetString(ctx.FlagValues, "bitbucket-host")

	flags := gitexporter.Flags{
		NoPR:         cmdctx.GetBool(ctx.FlagValues, "no-pr"),
		NoComment:    cmdctx.GetBool(ctx.FlagValues, "no-comment"),
		NoPRMetadata: cmdctx.GetBool(ctx.FlagValues, "no-pr-metadata"),
		NoWebhook:    cmdctx.GetBool(ctx.FlagValues, "no-webhook"),
		NoRule:       cmdctx.GetBool(ctx.FlagValues, "no-rule"),
		NoLabel:      true, // bitbucket doesn't support native labels
		NoLFS:        cmdctx.GetBool(ctx.FlagValues, "no-lfs"),
	}

	client, err := newBitbucketClient(host, token)
	if err != nil {
		return err
	}

	tracer := bridge.NewTracer(cmdctx.GetBool(ctx.FlagValues, "no-progress"))
	defer tracer.Close()

	checkpointManager := checkpoint.NewCheckpointManager(dir)
	if cmdctx.GetBool(ctx.FlagValues, "resume") {
		if err := checkpointManager.LoadCheckpoint(); err != nil {
			return fmt.Errorf("unable to load checkpoint from %s: %w", dir, err)
		}
	}

	fileLogger := &gitexporter.FileLogger{Location: dir}
	reporter := make(map[string]*report.Report)

	e := bitbucket.New(client, workspace, repository, checkpointManager, fileLogger, tracer, reporter)
	// x-token-auth is the git-clone username Bitbucket Cloud expects when
	// authenticating with an app token rather than a real account.
	exporter := gitexporter.NewExporter(e, dir, "x-token-auth", token, tracer, reporter, flags)

	bgCtx, cancel := bridge.WithInterrupt(ctx.Context)
	defer cancel()

	hlog.Debug("starting bitbucket git-export", "workspace", workspace, "repository", repository, "dir", dir)
	return exporter.Export(bgCtx)
}

// newBitbucketClient builds an scm client that injects the token as a bearer
// token, matching the standalone command's transport.
func newBitbucketClient(host, token string) (*scm.Client, error) {
	var client *scm.Client
	var err error
	if host != "" {
		client, err = scmbitbucket.New(host)
		if err != nil {
			return nil, err
		}
	} else {
		client = scmbitbucket.NewDefault()
	}
	client.Client = oauth2BearerClient(token)
	return client, nil
}
