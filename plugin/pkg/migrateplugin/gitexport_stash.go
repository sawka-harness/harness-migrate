// Copyright © 2026 Harness Inc.
// SPDX-License-Identifier: Apache-2.0

package migrateplugin

import (
	"fmt"
	"net/http"
	"strings"

	scmstash "github.com/drone/go-scm/scm/driver/stash"
	"github.com/drone/go-scm/scm/transport"

	"github.com/harness/cli/pkg/cmdctx"
	"github.com/harness/cli/pkg/hlog"

	"github.com/harness/harness-migrate/internal/checkpoint"
	"github.com/harness/harness-migrate/internal/gitexporter"
	"github.com/harness/harness-migrate/internal/migrate/stash"
	"github.com/harness/harness-migrate/internal/report"
	"github.com/harness/harness-migrate/plugin/pkg/bridge"
)

const executeGitExportStashID = "execute_git_export_stash"

// executeGitExportStash exports a Bitbucket Server (Stash) project's git data
// to a local folder. It calls the same engine constructors as the standalone
// `harness-migrate stash git-export` command; only the front-end differs.
func executeGitExportStash(ctx *cmdctx.Ctx) error {
	dir := cmdctx.GetString(ctx.FlagValues, "dir")
	// stash-host, stash-project, stash-token and stash-user are declared
	// required in the spec, so cobra has already rejected the run if any is
	// missing.
	host := cmdctx.GetString(ctx.FlagValues, "stash-host")
	project := strings.Trim(cmdctx.GetString(ctx.FlagValues, "stash-project"), "/")
	token := cmdctx.GetString(ctx.FlagValues, "stash-token")
	user := cmdctx.GetString(ctx.FlagValues, "stash-user")
	repository := strings.Trim(cmdctx.GetString(ctx.FlagValues, "stash-repo"), "/")

	flags := gitexporter.Flags{
		NoPR:         cmdctx.GetBool(ctx.FlagValues, "no-pr"),
		NoComment:    cmdctx.GetBool(ctx.FlagValues, "no-comment"),
		NoPRMetadata: cmdctx.GetBool(ctx.FlagValues, "no-pr-metadata"),
		NoWebhook:    cmdctx.GetBool(ctx.FlagValues, "no-webhook"),
		NoRule:       cmdctx.GetBool(ctx.FlagValues, "no-rule"),
		NoLabel:      true, // stash doesn't support labels
		NoLFS:        cmdctx.GetBool(ctx.FlagValues, "no-lfs"),
	}

	client, err := scmstash.New(host)
	if err != nil {
		return err
	}
	client.Client = &http.Client{
		Transport: &transport.BasicAuth{
			Username: user,
			Password: token,
		},
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

	e := stash.New(client, project, repository, checkpointManager, fileLogger, tracer, reporter)
	exporter := gitexporter.NewExporter(e, dir, user, token, tracer, reporter, flags)

	bgCtx, cancel := bridge.WithInterrupt(ctx.Context)
	defer cancel()

	hlog.Debug("starting stash git-export", "project", project, "repository", repository, "dir", dir)
	return exporter.Export(bgCtx)
}
