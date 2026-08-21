// Copyright © 2026 Harness Inc.
// SPDX-License-Identifier: Apache-2.0

package migrateplugin

import (
	"fmt"
	"strings"

	"github.com/drone/go-scm/scm"
	scmgitlab "github.com/drone/go-scm/scm/driver/gitlab"

	"github.com/harness/cli/pkg/cmdctx"
	"github.com/harness/cli/pkg/hlog"

	"github.com/harness/harness-migrate/internal/checkpoint"
	"github.com/harness/harness-migrate/internal/gitexporter"
	"github.com/harness/harness-migrate/internal/migrate/gitlab"
	"github.com/harness/harness-migrate/internal/report"
	"github.com/harness/harness-migrate/plugin/pkg/bridge"
)

const executeGitExportGitlabID = "execute_git_export_gitlab"

// executeGitExportGitlab exports a GitLab group's git data to a local folder. It
// calls the same engine constructors as the standalone `harness-migrate gitlab
// git-export` command; only the front-end differs.
func executeGitExportGitlab(ctx *cmdctx.Ctx) error {
	dir := cmdctx.GetString(ctx.FlagValues, "dir")
	// gitlab-group and gitlab-token are declared required in the spec, so cobra
	// has already rejected the run if either is missing.
	group := strings.Trim(cmdctx.GetString(ctx.FlagValues, "gitlab-group"), "/")
	token := cmdctx.GetString(ctx.FlagValues, "gitlab-token")
	project := strings.Trim(cmdctx.GetString(ctx.FlagValues, "gitlab-project"), "/")
	user := cmdctx.GetString(ctx.FlagValues, "gitlab-user")
	host := cmdctx.GetString(ctx.FlagValues, "gitlab-host")
	includeSubgroups := cmdctx.GetBool(ctx.FlagValues, "include-subgroups")

	flags := gitexporter.Flags{
		NoPR:         cmdctx.GetBool(ctx.FlagValues, "no-pr"),
		NoComment:    cmdctx.GetBool(ctx.FlagValues, "no-comment"),
		NoPRMetadata: cmdctx.GetBool(ctx.FlagValues, "no-pr-metadata"),
		NoWebhook:    cmdctx.GetBool(ctx.FlagValues, "no-webhook"),
		NoRule:       cmdctx.GetBool(ctx.FlagValues, "no-rule"),
		NoLabel:      cmdctx.GetBool(ctx.FlagValues, "no-label"),
		NoLFS:        cmdctx.GetBool(ctx.FlagValues, "no-lfs"),
	}

	client, err := newGitlabClient(host, token)
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

	e := gitlab.New(client, group, project, checkpointManager, fileLogger, tracer, reporter, includeSubgroups)
	exporter := gitexporter.NewExporter(e, dir, user, token, tracer, reporter, flags)

	bgCtx, cancel := bridge.WithInterrupt(ctx.Context)
	defer cancel()

	hlog.Debug("starting gitlab git-export", "group", group, "project", project, "dir", dir)
	return exporter.Export(bgCtx)
}

// newGitlabClient builds an scm client that injects the token as a bearer
// token, matching the standalone command's transport.
func newGitlabClient(host, token string) (*scm.Client, error) {
	var client *scm.Client
	var err error
	if host != "" {
		client, err = scmgitlab.New(host)
		if err != nil {
			return nil, err
		}
	} else {
		client = scmgitlab.NewDefault()
	}
	client.Client = oauth2BearerClient(token)
	return client, nil
}
